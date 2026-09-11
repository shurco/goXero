package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

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

// SyncConnection pulls statement lines for every account on the connection and
// upserts them into the staging inbox. Response is a summary tally.
func (h *BankFeedHandler) SyncConnection(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	conn, err := h.getOwnedConnection(c, orgID)
	if err != nil {
		return err
	}
	fetched, newLines, err := h.syncConnection(c.Context(), orgID, conn)
	if err != nil {
		// resolveProvider returns *fiber.Error (400/404); httpError would mask
		// those as 500, so pass fiber errors through unchanged.
		var fe *fiber.Error
		if errors.As(err, &fe) {
			return fe
		}
		return httpError(err)
	}
	return c.JSON(fiber.Map{"Fetched": fetched, "NewLines": newLines})
}

// getOwnedConnection loads the connection named by the :id route parameter and
// checks it belongs to the caller's tenant.
func (h *BankFeedHandler) getOwnedConnection(c fiber.Ctx, orgID uuid.UUID) (*models.BankFeedConnection, error) {
	id, err := parseID(c, "id")
	if err != nil {
		return nil, err
	}
	conn, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, id)
	if err != nil {
		return nil, httpError(err)
	}
	if conn.Status != models.BankFeedStatusLinked {
		return nil, fiber.NewError(fiber.StatusBadRequest, "connection is not linked")
	}
	return conn, nil
}

// syncConnection is the shared core of the manual "Sync" button and the
// background poller: it pulls the sync window for every account on the
// connection and upserts each line idempotently. The provider's own error is
// recorded on the connection so the UI can explain why a feed went quiet.
func (h *BankFeedHandler) syncConnection(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection) (fetched, newLines int, err error) {
	p, err := h.resolveProvider(conn.Provider)
	if err != nil {
		return 0, 0, err
	}
	// GetConnection always loads the connection's accounts eagerly, so this is
	// already the complete list.
	accounts := conn.Accounts

	to := time.Now().UTC()
	from := to.Add(-h.syncWindow)

	for _, a := range accounts {
		lines, err := p.FetchStatementLines(ctx, a.ExternalAccountID, from, to)
		if err != nil {
			_ = h.repos.BankFeeds.UpdateConnectionStatus(ctx, orgID, conn.ConnectionID,
				models.BankFeedStatusError, err.Error(), nil)
			return fetched, newLines, err
		}
		for _, l := range lines {
			feedAccountID := a.FeedAccountID
			row := &models.BankStatementLine{
				FeedAccountID: &feedAccountID,
				Source:        models.StatementLineSourceFeed,
				ProviderTxID:  l.ProviderTxID,
				PostedAt:      l.PostedAt,
				Amount:        l.Amount,
				CurrencyCode:  l.CurrencyCode,
				// The provider's counterparty is what the user sees in the
				// payee column; the description stays as the bank sent it.
				Payee:        l.Counterparty,
				Description:  l.Description,
				Counterparty: l.Counterparty,
				Reference:    l.Reference,
			}
			inserted, err := h.repos.BankFeeds.UpsertStatementLine(ctx, orgID, a.FeedAccountID, row, l.Raw)
			if err != nil {
				return fetched, newLines, err
			}
			fetched++
			if inserted {
				newLines++
			}
		}
	}
	now := time.Now().UTC()
	if err := h.repos.BankFeeds.UpdateConnectionStatus(ctx, orgID, conn.ConnectionID,
		models.BankFeedStatusLinked, "", &now); err != nil {
		return fetched, newLines, err
	}
	return fetched, newLines, nil
}

// SyncAllConnections is the background sweep: every linked connection of every
// tenant is refreshed once. One tenant's broken consent must not stop the rest,
// so failures are counted and logged rather than returned.
func (h *BankFeedHandler) SyncAllConnections(ctx context.Context) (synced, failed int) {
	conns, err := h.repos.BankFeeds.ListLinkedConnections(ctx)
	if err != nil {
		slog.Error("bank feed sweep: could not list connections", "err", err)
		return 0, 0
	}
	for _, c := range conns {
		if ctx.Err() != nil {
			return synced, failed
		}
		conn, err := h.repos.BankFeeds.GetConnection(ctx, c.OrganisationID, c.ConnectionID)
		if err != nil {
			slog.Error("bank feed sweep: could not load connection",
				"connectionId", c.ConnectionID, "err", err)
			failed++
			continue
		}
		fetched, newLines, err := h.syncConnection(ctx, c.OrganisationID, conn)
		if err != nil {
			slog.Warn("bank feed sweep: sync failed",
				"connectionId", c.ConnectionID, "provider", c.Provider, "err", err)
			failed++
			continue
		}
		synced++
		slog.Info("bank feed sweep: synced",
			"connectionId", c.ConnectionID, "fetched", fetched, "newLines", newLines)
	}
	return synced, failed
}

// StartSyncScheduler polls every linked connection on an interval until the
// context is cancelled. An interval of zero disables it, which is what tests
// and single-tenant deployments want.
func (h *BankFeedHandler) StartSyncScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.SyncAllConnections(ctx)
			}
		}
	}()
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
