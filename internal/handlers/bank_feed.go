package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/bankfeed"
	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// BankFeedHandler exposes the Open Banking integration: list providers/institutions,
// create a consent link, finalise after the user returns, then poll providers
// to populate a per-tenant staging inbox of statement lines.
//
// Design: the handler never stores bank credentials; it only keeps the opaque
// provider reference (GoCardless requisition_id, Plaid item_id, …) plus the
// statement rows returned after consent.
type BankFeedHandler struct {
	repos       *repository.Repositories
	providers   *bankfeed.Registry
	redirectURL string
	syncWindow  time.Duration // how far back to pull on each sync
}

// NewBankFeedHandler wires the dependencies. `redirectURL` is where providers
// should send the browser after consent; `syncWindow` caps how far back we
// pull statement lines per sync (default 90d keeps us within GoCardless free
// tier limits).
func NewBankFeedHandler(repos *repository.Repositories, reg *bankfeed.Registry, redirectURL string, syncWindow time.Duration) *BankFeedHandler {
	if syncWindow <= 0 {
		syncWindow = 90 * 24 * time.Hour
	}
	return &BankFeedHandler{repos: repos, providers: reg, redirectURL: redirectURL, syncWindow: syncWindow}
}

// ListProviders returns the slugs of providers registered at boot so the UI
// can render only options that have credentials configured.
func (h *BankFeedHandler) ListProviders(c fiber.Ctx) error {
	return rawList(c, fiber.StatusOK, "Providers", h.providers.Names())
}

// ListInstitutions proxies the provider-native institution catalogue.
// Required query: ?provider=&country=.
func (h *BankFeedHandler) ListInstitutions(c fiber.Ctx) error {
	p, err := h.resolveProvider(c.Query("provider"))
	if err != nil {
		return err
	}
	country := c.Query("country")
	items, err := p.ListInstitutions(c.Context(), country)
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Institutions", items)
}

// CreateConnection starts the consent flow: stores a PENDING row locally,
// calls the provider to obtain an auth URL, then persists the external
// reference. Response is the connection record (with AuthURL populated).
func (h *BankFeedHandler) CreateConnection(c fiber.Ctx) error {
	body, err := bindBody[struct {
		Provider        string `json:"Provider"`
		InstitutionID   string `json:"InstitutionID"`
		InstitutionName string `json:"InstitutionName"`
		RedirectURL     string `json:"RedirectURL"`
	}](c)
	if err != nil {
		return err
	}
	if body.Provider == "" || body.InstitutionID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Provider and InstitutionID are required")
	}
	p, err := h.resolveProvider(body.Provider)
	if err != nil {
		return err
	}
	orgID := middleware.OrganisationIDFrom(c)

	conn := &models.BankFeedConnection{
		Provider:        body.Provider,
		Status:          models.BankFeedStatusPending,
		InstitutionID:   body.InstitutionID,
		InstitutionName: body.InstitutionName,
	}
	if err := h.repos.BankFeeds.CreateConnection(c.Context(), orgID, conn); err != nil {
		return httpError(err)
	}

	redirect := firstNonBlank(body.RedirectURL, h.redirectURL)
	session, err := p.CreateSession(c.Context(), bankfeed.SessionRequest{
		InstitutionID: body.InstitutionID,
		RedirectURL:   redirect,
		Reference:     conn.ConnectionID.String(),
	})
	if err != nil {
		_ = h.repos.BankFeeds.UpdateConnectionStatus(c.Context(), orgID, conn.ConnectionID,
			models.BankFeedStatusError, err.Error(), nil)
		return httpError(err)
	}
	conn.ExternalReference = session.ExternalReference
	conn.AuthURL = session.AuthURL
	updated, err := h.reloadConnection(c.Context(), orgID, conn.ConnectionID, conn.ExternalReference, conn.AuthURL)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "Connections", *updated)
}

// ListConnections returns all connections + their discovered accounts.
func (h *BankFeedHandler) ListConnections(c fiber.Ctx) error {
	items, err := h.repos.BankFeeds.ListConnections(c.Context(), middleware.OrganisationIDFrom(c))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Connections", items)
}

// GetConnection returns a single connection (useful for polling after the
// user returns from the bank).
func (h *BankFeedHandler) GetConnection(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return err
	}
	conn, err := h.repos.BankFeeds.GetConnection(c.Context(), middleware.OrganisationIDFrom(c), id)
	if err != nil {
		return httpError(err)
	}
	return envelopeOne(c, "Connections", *conn)
}

// DeleteConnection drops the connection and everything beneath it.
func (h *BankFeedHandler) DeleteConnection(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return err
	}
	if err := h.repos.BankFeeds.DeleteConnection(c.Context(), middleware.OrganisationIDFrom(c), id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// FinalizeConnection is called after the user returns from the bank consent
// UI. It asks the provider for the now-linked accounts and persists them.
func (h *BankFeedHandler) FinalizeConnection(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return err
	}
	orgID := middleware.OrganisationIDFrom(c)
	conn, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if conn.ExternalReference == "" {
		return fiber.NewError(fiber.StatusBadRequest, "connection has no external reference")
	}
	p, err := h.resolveProvider(conn.Provider)
	if err != nil {
		return err
	}
	accounts, err := p.FinalizeSession(c.Context(), conn.ExternalReference)
	if err != nil {
		_ = h.repos.BankFeeds.UpdateConnectionStatus(c.Context(), orgID, id,
			models.BankFeedStatusError, err.Error(), nil)
		return httpError(err)
	}
	for _, a := range accounts {
		fa := &models.BankFeedAccount{
			ConnectionID:      conn.ConnectionID,
			ExternalAccountID: a.ExternalID,
			DisplayName:       a.DisplayName,
			IBAN:              a.IBAN,
			CurrencyCode:      a.CurrencyCode,
			Balance:           a.Balance,
		}
		if err := h.repos.BankFeeds.UpsertAccount(c.Context(), orgID, fa); err != nil {
			return httpError(err)
		}
	}
	if err := h.repos.BankFeeds.UpdateConnectionStatus(c.Context(), orgID, id,
		models.BankFeedStatusLinked, "", nil); err != nil {
		return httpError(err)
	}
	refreshed, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Connections", *refreshed)
}

// SyncConnection pulls statement lines for every account on the connection
// and upserts them into the staging table. Response is a summary tally.
func (h *BankFeedHandler) SyncConnection(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return err
	}
	orgID := middleware.OrganisationIDFrom(c)
	conn, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if conn.Status != models.BankFeedStatusLinked {
		return fiber.NewError(fiber.StatusBadRequest, "connection is not linked")
	}
	p, err := h.resolveProvider(conn.Provider)
	if err != nil {
		return err
	}

	to := time.Now().UTC()
	from := to.Add(-h.syncWindow)

	var totalFetched, totalNew int
	for _, a := range conn.Accounts {
		lines, err := p.FetchStatementLines(c.Context(), a.ExternalAccountID, from, to)
		if err != nil {
			_ = h.repos.BankFeeds.UpdateConnectionStatus(c.Context(), orgID, id,
				models.BankFeedStatusError, err.Error(), nil)
			return httpError(err)
		}
		for _, l := range lines {
			row := &models.BankFeedStatementLine{
				FeedAccountID: a.FeedAccountID,
				ProviderTxID:  l.ProviderTxID,
				PostedAt:      l.PostedAt,
				Amount:        l.Amount,
				CurrencyCode:  l.CurrencyCode,
				Description:   l.Description,
				Counterparty:  l.Counterparty,
				Reference:     l.Reference,
				Status:        models.BankFeedLineStatusNew,
			}
			inserted, err := h.repos.BankFeeds.UpsertStatementLine(c.Context(), orgID, a.FeedAccountID, row, l.Raw)
			if err != nil {
				return httpError(err)
			}
			totalFetched++
			if inserted {
				totalNew++
			}
		}
	}
	now := time.Now().UTC()
	if err := h.repos.BankFeeds.UpdateConnectionStatus(c.Context(), orgID, id,
		models.BankFeedStatusLinked, "", &now); err != nil {
		return httpError(err)
	}
	return c.JSON(fiber.Map{"Fetched": totalFetched, "NewLines": totalNew})
}

// BindFeedAccount links a feed account to an existing BANK ledger account.
// Pass "AccountID": null to unbind.
func (h *BankFeedHandler) BindFeedAccount(c fiber.Ctx) error {
	feedID, err := parseID(c, "feedAccountId")
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		AccountID *uuid.UUID `json:"AccountID"`
	}](c)
	if err != nil {
		return err
	}
	if err := h.repos.BankFeeds.BindAccount(c.Context(), middleware.OrganisationIDFrom(c), feedID, body.AccountID); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// ListStatementLines returns the staging inbox. Filters:
//
//	?feedAccountId=<uuid>
//	?status=NEW|IMPORTED|IGNORED   (default NEW)
func (h *BankFeedHandler) ListStatementLines(c fiber.Ctx) error {
	var feedAccountID *uuid.UUID
	if raw := c.Query("feedAccountId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid feedAccountId")
		}
		feedAccountID = &id
	}
	status := c.Query("status", models.BankFeedLineStatusNew)
	p := paginationFromQuery(c)
	items, total, err := h.repos.BankFeeds.ListStatementLines(c.Context(), middleware.OrganisationIDFrom(c), feedAccountID, status, p)
	if err != nil {
		return httpError(err)
	}
	p.Total = total
	return c.JSON(fiber.Map{"StatementLines": items, "Pagination": p})
}

// ImportStatementLine materialises a staging row as a bank_transaction so it
// flows into GL + P&L. The caller optionally overrides the destination
// account / contact; when omitted we fall back to the feed's linked account.
func (h *BankFeedHandler) ImportStatementLine(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return err
	}
	body, err := bindBody[struct {
		BankAccountID *uuid.UUID       `json:"BankAccountID"`
		ContactID     *uuid.UUID       `json:"ContactID"`
		AccountCode   string           `json:"AccountCode"`
		Reference     string           `json:"Reference"`
		TaxAmount     *decimal.Decimal `json:"TaxAmount"`
	}](c)
	if err != nil {
		return err
	}
	orgID := middleware.OrganisationIDFrom(c)

	line, err := h.repos.BankFeeds.GetStatementLine(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if line.Status == models.BankFeedLineStatusImported {
		return fiber.NewError(fiber.StatusConflict, "statement line already imported")
	}

	bankAccountID := body.BankAccountID
	if bankAccountID == nil {
		fa, err := h.repos.BankFeeds.GetFeedAccount(c.Context(), orgID, line.FeedAccountID)
		if err != nil {
			return httpError(err)
		}
		if fa.AccountID == nil {
			return fiber.NewError(fiber.StatusBadRequest, "feed account is not bound to a bank account; pass BankAccountID")
		}
		bankAccountID = fa.AccountID
	}

	txType := models.BankTransactionTypeReceive
	amount := line.Amount
	if amount.IsNegative() {
		txType = models.BankTransactionTypeSpend
		amount = amount.Neg()
	}
	posted := line.PostedAt
	bt := &models.BankTransaction{
		Type:            txType,
		BankAccountID:   bankAccountID,
		ContactID:       body.ContactID,
		Date:            &posted,
		Reference:       firstNonBlank(body.Reference, line.Reference, line.Description),
		CurrencyCode:    line.CurrencyCode,
		Status:          "AUTHORISED",
		LineAmountTypes: models.LineAmountTypesInclusive,
		LineItems: []models.LineItem{{
			Description: line.Description,
			Quantity:    decimal.NewFromInt(1),
			UnitAmount:  amount,
			AccountCode: firstNonBlank(body.AccountCode, "200"),
			TaxAmount:   zeroIfNil(body.TaxAmount),
		}},
	}
	if err := h.repos.BankTransactions.Create(c.Context(), orgID, bt); err != nil {
		return httpError(err)
	}
	if err := h.repos.BankFeeds.MarkLineImported(c.Context(), orgID, id, bt.BankTransactionID); err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusCreated, "BankTransactions", *bt)
}

// IgnoreStatementLine hides a row from the inbox.
func (h *BankFeedHandler) IgnoreStatementLine(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return err
	}
	if err := h.repos.BankFeeds.MarkLineIgnored(c.Context(), middleware.OrganisationIDFrom(c), id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

func (h *BankFeedHandler) resolveProvider(name string) (bankfeed.Provider, error) {
	if name == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "provider is required")
	}
	p, err := h.providers.Get(name)
	if err != nil {
		if errors.Is(err, bankfeed.ErrProviderNotRegistered) {
			return nil, fiber.NewError(fiber.StatusNotFound, "provider not registered or missing credentials")
		}
		return nil, err
	}
	return p, nil
}

// reloadConnection persists the provider-returned identifiers and re-reads
// the row so callers can return the fully materialised state.
func (h *BankFeedHandler) reloadConnection(ctx context.Context, orgID, connID uuid.UUID, extRef, authURL string) (*models.BankFeedConnection, error) {
	if _, err := h.repos.Pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET external_reference = NULLIF($3,''), auth_url = NULLIF($4,''), updated_at = now()
		 WHERE organisation_id = $1 AND connection_id = $2`,
		orgID, connID, extRef, authURL); err != nil {
		return nil, err
	}
	return h.repos.BankFeeds.GetConnection(ctx, orgID, connID)
}

func firstNonBlank(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func zeroIfNil(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}
