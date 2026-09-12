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
// statement rows returned after consent. A connection's consent can be finished
// in three ways — the browser returning to our redirect URL, the provider's
// webhook, or a repair session — and all three are the same code path, because
// they have to leave the row in exactly the same state.
type BankFeedHandler struct {
	repos     *repository.Repositories
	providers *bankfeed.Registry
	// secrets seals the per-connection credentials providers issue during
	// consent (Plaid Item access tokens). Nil when BANKFEED_ENCRYPTION_KEY is
	// unset — only providers that store no secret can run in that state.
	secrets     *bankfeed.SecretBox
	redirectURL string
	syncWindow  time.Duration // how far back to pull on each sync
	// reconciler is the reconcile inbox's automatic pass, run after a sync for
	// every account the new lines landed on. Optional: without it a sync still
	// imports lines, it just leaves them all for someone to match by hand.
	reconciler AutoReconciler
}

// AutoReconciler is the slice of the statement-line handler a feed sync needs:
// the account's automatic reconcile pass, which does nothing unless the account
// has the setting switched on. It is an interface rather than the handler itself
// so the feed stays testable without the reconcile screen behind it.
type AutoReconciler interface {
	AutoReconcileIfEnabled(ctx context.Context, orgID, accountID uuid.UUID) (matched int, err error)
}

// NewBankFeedHandler wires the dependencies. `redirectURL` is where providers
// should send the browser after consent; `syncWindow` caps how far back we pull
// statement lines per date-window sync (default 90d). Providers that sync by
// cursor ignore it.
func NewBankFeedHandler(repos *repository.Repositories, reg *bankfeed.Registry, secrets *bankfeed.SecretBox, redirectURL string, syncWindow time.Duration) *BankFeedHandler {
	if syncWindow <= 0 {
		syncWindow = 90 * 24 * time.Hour
	}
	return &BankFeedHandler{repos: repos, providers: reg, secrets: secrets, redirectURL: redirectURL, syncWindow: syncWindow}
}

// SetAutoReconciler wires in the inbox's automatic reconcile pass. Called from
// the router once both handlers exist; a handler without one simply never runs
// it.
func (h *BankFeedHandler) SetAutoReconciler(r AutoReconciler) { h.reconciler = r }

// providerOption is a registered provider plus which optional flows its adapter
// implements. The UI decides what to offer from these: it cannot inspect the
// adapter, and guessing from the slug is how a provider ends up with a button
// that only ever answers 400.
type providerOption struct {
	Slug string `json:"Slug"`
	// PicksInstitution means the provider's own consent flow can ask which bank
	// to use, so the user need not have chosen one here first.
	PicksInstitution bool `json:"PicksInstitution"`
	// CanRepair means a broken consent can be re-authenticated on the connection
	// that holds it, rather than connecting the same account a second time.
	CanRepair bool `json:"CanRepair"`
}

// ListProviders returns the providers registered at boot so the UI can render
// only the options that have credentials configured, and only the flows each one
// can actually run.
func (h *BankFeedHandler) ListProviders(c fiber.Ctx) error {
	registered := h.providers.All()
	out := make([]providerOption, 0, len(registered))
	for _, p := range registered {
		opt := providerOption{Slug: p.Name()}
		// Type-asserted the same way the handlers that enforce these are: the
		// interface being present is not enough, its answer is what counts.
		if picker, ok := p.(bankfeed.InstitutionPicker); ok {
			opt.PicksInstitution = picker.PicksInstitution()
		}
		_, opt.CanRepair = p.(bankfeed.ReconsentProvider)
		out = append(out, opt)
	}
	return rawList(c, fiber.StatusOK, "Providers", out)
}

// ListInstitutions proxies the provider-native institution catalogue.
// Required query: ?provider=&country=.
func (h *BankFeedHandler) ListInstitutions(c fiber.Ctx) error {
	p, err := h.resolveProvider(c.Query("provider"))
	if err != nil {
		return err
	}
	// `q` is answered upstream where the catalogue is too large to enumerate
	// (Plaid), and locally where it is not (GoCardless).
	items, err := p.ListInstitutions(c.Context(), c.Query("country"), c.Query("q"))
	if err != nil {
		return httpError(err)
	}
	return envelopeList(c, "Institutions", items)
}

// CreateConnection starts the consent flow: stores a PENDING row locally,
// calls the provider to obtain an auth URL, then persists the session.
// Response is the connection record (with AuthURL populated).
// refuseDuplicateInstitution keeps one live connection per bank. A second one is
// not another way in: the provider mints a second identity for the same
// accounts, every transaction is staged twice, and the ledger gets a second bank
// account for money that already has one. So an institution that already has a
// PENDING or LINKED connection is refused with what to do instead — the answer
// is the connection that exists (finish it, repair it) or disconnecting it.
//
// `except` excludes the connection being worked on, so finalizing a repair does
// not refuse the bank it is repairing.
func (h *BankFeedHandler) refuseDuplicateInstitution(ctx context.Context, orgID uuid.UUID, provider, institutionID, institutionName string, except uuid.UUID) error {
	if institutionID == "" {
		return nil
	}
	other, err := h.repos.BankFeeds.LiveConnectionAtInstitution(ctx, orgID, provider, institutionID, except)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return httpError(err)
	}
	name := firstNonBlank(other.InstitutionName, institutionName, institutionID)
	if other.Status == models.BankFeedStatusPending {
		return fiber.NewError(fiber.StatusConflict,
			name+" is already waiting for consent — finish that connection instead of starting a second one")
	}
	return fiber.NewError(fiber.StatusConflict,
		name+" is already connected — link it again and every transaction would be imported twice, "+
			"so use that connection or disconnect it first")
}

func (h *BankFeedHandler) CreateConnection(c fiber.Ctx) error {
	body, err := bindBody[struct {
		Provider        string `json:"Provider"`
		InstitutionID   string `json:"InstitutionID"`
		InstitutionName string `json:"InstitutionName"`
		Country         string `json:"Country"`
		RedirectURL     string `json:"RedirectURL"`
	}](c)
	if err != nil {
		return err
	}
	if body.Provider == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Provider is required")
	}
	p, err := h.resolveProvider(body.Provider)
	if err != nil {
		return err
	}
	// An institution is what our own picker sends, and pinning it skips the
	// provider's. A provider whose consent flow can run without one lets the user
	// choose there instead — the only way in on a Plaid account that refuses a
	// pinned institution, and the way to reach an institution our catalogue has
	// not listed. For everybody else the field stays mandatory.
	if body.InstitutionID == "" {
		picker, ok := p.(bankfeed.InstitutionPicker)
		if !ok || !picker.PicksInstitution() {
			return fiber.NewError(fiber.StatusBadRequest, "InstitutionID is required for this provider")
		}
	}
	orgID := middleware.OrganisationIDFrom(c)
	// Only when the request names the bank: a provider that picks the institution
	// inside its own flow has not told us which one yet, and persistConsent is
	// where that becomes knowable.
	if err := h.refuseDuplicateInstitution(c.Context(), orgID,
		body.Provider, body.InstitutionID, body.InstitutionName, uuid.Nil); err != nil {
		return err
	}

	conn := &models.BankFeedConnection{
		Provider:        body.Provider,
		Status:          models.BankFeedStatusPending,
		InstitutionID:   body.InstitutionID,
		InstitutionName: body.InstitutionName,
		Country:         body.Country,
	}
	if err := h.repos.BankFeeds.CreateConnection(c.Context(), orgID, conn); err != nil {
		return httpError(err)
	}

	redirect := firstNonBlank(body.RedirectURL, h.redirectURL)
	session, err := p.CreateSession(c.Context(), bankfeed.SessionRequest{
		InstitutionID: body.InstitutionID,
		Country:       body.Country,
		RedirectURL:   redirect,
		Reference:     conn.ConnectionID.String(),
	})
	if err != nil {
		_ = h.repos.BankFeeds.UpdateConnectionStatus(c.Context(), orgID, conn.ConnectionID,
			models.BankFeedStatusError, err.Error(), nil)
		return httpError(err)
	}
	conn.AuthURL = session.AuthURL
	if body.InstitutionID != "" && !session.InstitutionPinned {
		// Worth a line in the log: the user is about to see a bank picker they did
		// not ask for, and it is the provider's account that decided so.
		slog.Warn("bank feed: provider refused the pinned institution",
			"provider", body.Provider, "institution", body.InstitutionID)
	}
	// The session handle and its URL are all we hold until consent is actually
	// granted: the provider has issued nothing durable yet. FinalizeConnection —
	// or the provider's own webhook — turns this into the real thing.
	if err := h.repos.BankFeeds.SetConnectionSession(c.Context(), orgID, conn.ConnectionID,
		session.ExternalReference, session.AuthURL); err != nil {
		return httpError(err)
	}
	updated, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, conn.ConnectionID)
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
	orgID := middleware.OrganisationIDFrom(c)
	// The row is what names the provider identity, so it is read and given up
	// while the row still exists.
	conn, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	h.revokeConsent(c.Context(), conn)
	if err := h.repos.BankFeeds.DeleteConnection(c.Context(), orgID, id); err != nil {
		return httpError(err)
	}
	return noContent(c)
}

// revokeConsent tells the provider that a consent is over, before the row that
// names it is deleted. It is deliberately best-effort: a provider that cannot be
// reached, or that refuses, must not stop the user from disconnecting a bank —
// the alternative is a connection they cannot get rid of while the provider is
// down. What it buys when it works is that nothing of ours is left live at the
// bank: no Item counting against an account limit, no token that still opens
// somebody's transactions.
func (h *BankFeedHandler) revokeConsent(ctx context.Context, conn *models.BankFeedConnection) {
	p, err := h.resolveProvider(conn.Provider)
	if err != nil {
		return
	}
	revoker, ok := p.(bankfeed.ConsentRevoker)
	if !ok {
		return
	}
	cred, err := h.credentialFor(conn)
	if err != nil {
		slog.Warn("bank feed: cannot read the credential to revoke",
			"provider", conn.Provider, "connectionId", conn.ConnectionID, "err", err)
		return
	}
	if err := revoker.RevokeConsent(ctx, cred); err != nil {
		slog.Warn("bank feed: provider would not revoke the consent",
			"provider", conn.Provider, "connectionId", conn.ConnectionID, "err", err)
	}
}

// FinalizeConnection is called after the user returns from the bank consent
// UI — or by the callback screen, which does the same thing from the browser.
// A connection that is already linked is returned as it is: the provider's
// webhook may well have finished the session first.
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
	if conn.Status != models.BankFeedStatusLinked {
		if err := h.completeConsent(c.Context(), orgID, conn); err != nil {
			return err
		}
	}
	refreshed, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Connections", *refreshed)
}

// ReconnectConnection sends a broken connection back to the bank for repair.
//
// A connection in ERROR is usually one whose consent the bank has invalidated —
// Plaid reports those as ITEM_LOGIN_REQUIRED — and the fix is to re-authenticate
// the Item that already exists, not to create a second one: a fresh consent
// would import the same accounts again under a new connection and split their
// history in two. The session the provider hands back therefore goes onto this
// connection, which keeps its accounts and its sync cursor, and completing it
// only clears the error.
//
// Providers with no repair flow answer 400: for those the user disconnects and
// connects again, which is what their consent model expects anyway.
func (h *BankFeedHandler) ReconnectConnection(c fiber.Ctx) error {
	id, err := parseID(c, "id")
	if err != nil {
		return err
	}
	orgID := middleware.OrganisationIDFrom(c)
	conn, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	if !hasDurableConsent(conn) {
		return fiber.NewError(fiber.StatusBadRequest, "connection has no consent to repair yet")
	}
	if conn.Status == models.BankFeedStatusRevoked {
		// The user revoked us at the bank; only a fresh consent can bring the
		// feed back, and pretending otherwise would fail in the consent UI.
		return fiber.NewError(fiber.StatusConflict, "consent was revoked at the bank — connect a new one instead")
	}
	p, err := h.resolveProvider(conn.Provider)
	if err != nil {
		return err
	}
	reparer, ok := p.(bankfeed.ReconsentProvider)
	if !ok {
		return fiber.NewError(fiber.StatusBadRequest, "provider cannot repair an existing connection")
	}
	cred, err := h.credentialFor(conn)
	if err != nil {
		return httpError(err)
	}
	session, err := reparer.CreateReconsentSession(c.Context(), cred, bankfeed.SessionRequest{
		InstitutionID: conn.InstitutionID,
		// Plaid checks the countries against the Item being repaired, so the one
		// the user picked the institution under has to be replayed here.
		Country:     conn.Country,
		RedirectURL: h.redirectURL,
		Reference:   conn.ConnectionID.String(),
	})
	if err != nil {
		_ = h.repos.BankFeeds.UpdateConnectionStatus(c.Context(), orgID, conn.ConnectionID,
			models.BankFeedStatusError, err.Error(), nil)
		return httpError(err)
	}
	if err := h.repos.BankFeeds.SetConnectionSession(c.Context(), orgID, conn.ConnectionID,
		session.ExternalReference, session.AuthURL); err != nil {
		return httpError(err)
	}
	refreshed, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, id)
	if err != nil {
		return httpError(err)
	}
	return rawOne(c, fiber.StatusOK, "Connections", *refreshed)
}

// hasDurableConsent reports whether the connection already holds a provider
// identity plus the credential behind it. That is what makes the session it may
// be waiting on a *repair* of an existing consent rather than a first-time link
// — the distinction the completion path turns on.
func hasDurableConsent(conn *models.BankFeedConnection) bool {
	return conn.ExternalReference != "" && len(conn.ExternalSecret) > 0
}

// completeConsent finishes whichever session the connection is waiting on and
// persists the result. A provider error is recorded on the connection — but only
// while it is still unlinked, because the browser redirect and the webhook
// complete the same session and the loser of that race must not undo the
// winner's success.
func (h *BankFeedHandler) completeConsent(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection) error {
	p, err := h.resolveProvider(conn.Provider)
	if err != nil {
		return err
	}
	cred, err := h.credentialFor(conn)
	if err != nil {
		return httpError(err)
	}

	var consent *bankfeed.Consent
	if hasDurableConsent(conn) {
		reparer, ok := p.(bankfeed.ReconsentProvider)
		if !ok {
			return fiber.NewError(fiber.StatusBadRequest, "provider cannot repair an existing connection")
		}
		consent, err = reparer.CompleteReconsent(ctx, cred)
	} else {
		consent, err = p.FinalizeSession(ctx, cred)
	}
	if err != nil {
		// A consent the user has not finished yet is not a broken connection: the
		// session is still open at the bank, and marking the row ERROR here would
		// make one click too early look like a feed that stopped working.
		//
		// It is answered 409 rather than 400 so a caller can tell "the bank has not
		// said yes yet" from "this request is wrong": the redirect back from the
		// bank arrives whether or not the user approved access there, and the page
		// they land on has to say the honest thing — still waiting — instead of
		// reporting a failure of a connection that is perfectly fine.
		if errors.Is(err, bankfeed.ErrConsentNotFinished) {
			return fiber.NewError(fiber.StatusConflict,
				"the bank has not finished this consent yet — approve access there, then try again")
		}
		_ = h.repos.BankFeeds.FailFinalize(ctx, orgID, conn.ConnectionID, err.Error())
		return httpError(err)
	}
	return h.persistConsent(ctx, orgID, conn, consent)
}

// persistConsent stores what a finished consent flow produced: the accounts the
// user granted, the durable connection identity, and the sealed secret. The
// browser return, the provider's webhook and a repair all land here, so all
// three leave the row in exactly the same state.
func (h *BankFeedHandler) persistConsent(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection, consent *bankfeed.Consent) error {
	// Which bank the consent ended up at is only really known now — a session
	// that could not pin an institution leaves the choice to the provider's own
	// picker — so the one-live-connection-per-bank rule is checked again here,
	// against what the Item says. This is the case the create-time check cannot
	// catch, and the one where letting it through costs the most: every account
	// would be staged a second time, each under a second ledger account.
	if consent.Institution.ID != "" {
		if err := h.refuseDuplicateInstitution(ctx, orgID, conn.Provider,
			consent.Institution.ID, consent.Institution.Name, conn.ConnectionID); err != nil {
			// The consent itself is real — an Item now exists at the bank — but
			// this connection is not one we will use. That is recorded, and the
			// session is closed out with it: it is over, and leaving it open would
			// have the callback screen retrying a connection that can never link.
			_ = h.repos.BankFeeds.FailFinalize(ctx, orgID, conn.ConnectionID, err.Error())
			_ = h.repos.BankFeeds.SetConnectionSession(ctx, orgID, conn.ConnectionID, "", "")
			return err
		}
	}
	for _, a := range consent.Accounts {
		fa := &models.BankFeedAccount{
			ConnectionID:      conn.ConnectionID,
			ExternalAccountID: a.ExternalID,
			DisplayName:       a.DisplayName,
			IBAN:              a.IBAN,
			CurrencyCode:      a.CurrencyCode,
			Balance:           a.Balance,
		}
		if err := h.repos.BankFeeds.UpsertAccount(ctx, orgID, fa); err != nil {
			return httpError(err)
		}
		// The consent is what the user asked for; a ledger account is what the
		// books need, and the bank accounts screen shows the latter. Creating it
		// here — rather than leaving a binding nobody has a reason to make — is
		// what makes a connected bank turn up in that table.
		if _, err := h.repos.BankFeeds.ProvisionLedgerAccount(ctx, orgID, fa.FeedAccountID); err != nil {
			return httpError(err)
		}
	}
	// The institution is settled here rather than at CreateSession, because the
	// provider is the only one who knows which bank the consent ended up at: a
	// session that could not pin one leaves the choice to the user inside the
	// provider's own flow. When the id matches what the request named, the name
	// we already had is the right one to keep — a provider that reports an id
	// alone would otherwise leave the row showing nothing.
	if consent.Institution.ID != "" {
		institutionName := conn.InstitutionName
		if consent.Institution.ID != conn.InstitutionID {
			institutionName = firstNonBlank(consent.Institution.Name, consent.Institution.ID)
		}
		if err := h.repos.BankFeeds.SetConnectionInstitution(ctx, orgID, conn.ConnectionID,
			consent.Institution.ID, institutionName); err != nil {
			return httpError(err)
		}
	}
	// The provider may have traded the throwaway session handle for a durable
	// one (Plaid: link_token → item_id + access_token), so the identity is
	// persisted only now, once consent is actually in hand. A repair hands back
	// the same access token, which is re-sealed under a fresh nonce — same
	// plaintext, and no special case for the caller to make.
	sealed, err := h.sealSecret(consent.Secret)
	if err != nil {
		return httpError(err)
	}
	if err := h.repos.BankFeeds.SetConnectionConsent(ctx, orgID, conn.ConnectionID,
		consent.Reference, sealed); err != nil {
		return httpError(err)
	}
	if err := h.repos.BankFeeds.UpdateConnectionStatus(ctx, orgID, conn.ConnectionID,
		models.BankFeedStatusLinked, "", nil); err != nil {
		return httpError(err)
	}
	return nil
}

// SyncConnection pulls statement lines for every account on the connection and
// upserts them into the staging inbox. Response is a summary tally.
func (h *BankFeedHandler) SyncConnection(c fiber.Ctx) error {
	orgID := middleware.OrganisationIDFrom(c)
	conn, err := h.getOwnedConnection(c, orgID)
	if err != nil {
		return err
	}
	res, err := h.syncConnection(c.Context(), orgID, conn)
	if err != nil {
		// resolveProvider returns *fiber.Error (400/404); httpError would mask
		// those as 500, so pass fiber errors through unchanged.
		var fe *fiber.Error
		if errors.As(err, &fe) {
			return fe
		}
		return httpError(err)
	}
	return c.JSON(fiber.Map{
		"Fetched":         res.fetched,
		"NewLines":        res.newLines,
		"AutoMatched":     res.matched,
		"UpstreamChanges": res.upstream,
	})
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

// provisionMissingAccounts binds every feed account that has no ledger account
// behind it, creating that account. A failure is logged rather than raised: the
// sync's own work is worth more than the binding, the lines still stage under
// the feed account, and the next pass tries again.
func (h *BankFeedHandler) provisionMissingAccounts(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection) {
	for i := range conn.Accounts {
		if conn.Accounts[i].AccountID != nil {
			continue
		}
		accountID, err := h.repos.BankFeeds.ProvisionLedgerAccount(ctx, orgID, conn.Accounts[i].FeedAccountID)
		if err != nil {
			slog.Error("bank feed: could not open a ledger account for a connected bank account",
				"err", err, "connection", conn.ConnectionID, "feedAccount", conn.Accounts[i].FeedAccountID)
			continue
		}
		conn.Accounts[i].AccountID = &accountID
	}
}

// syncResult is what one sync of one connection did: how many lines came back,
// how many were new, how many the automatic pass reconciled on the way in, and
// how many lines under the connection the bank disagrees with. That last one is
// a standing count rather than a tally of this run — the question it answers is
// "is there something to look at?", not "what did this run change?".
type syncResult struct {
	fetched  int
	newLines int
	matched  int
	upstream int
	// accounts collects the ledger accounts that received lines, so the
	// automatic pass runs once per account rather than once per line.
	accounts map[uuid.UUID]bool
}

func newSyncResult() *syncResult { return &syncResult{accounts: map[uuid.UUID]bool{}} }

// syncConnection is the shared core of the manual "Sync" button and the
// background poller. Providers that sync by cursor get the incremental path;
// the rest are pulled over a rolling date window, once per account. Either way
// the provider's own error is recorded on the connection so the UI can explain
// why a feed went quiet.
func (h *BankFeedHandler) syncConnection(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection) (*syncResult, error) {
	p, err := h.resolveProvider(conn.Provider)
	if err != nil {
		return nil, err
	}
	cred, err := h.credentialFor(conn)
	if err != nil {
		return nil, err
	}
	// Before the pull, not after: an account the consent granted but nothing has
	// bound yet — a connection linked before provisioning existed, or one the
	// bank added an account to later — gets its ledger account here, so the lines
	// arriving in this same sync land on it.
	h.provisionMissingAccounts(ctx, orgID, conn)
	res := newSyncResult()
	if inc, ok := p.(bankfeed.IncrementalProvider); ok {
		err = h.syncIncremental(ctx, orgID, conn, inc, cred, res)
	} else {
		err = h.syncDateWindow(ctx, orgID, conn, p, cred, res)
	}
	if err != nil {
		h.recordSyncFailure(ctx, orgID, conn, p, err)
		return res, err
	}
	// The lines are in; now do what the user asked for by switching the account
	// to automatic. This is deliberately after the sync rather than part of it:
	// a match is worth having, but never at the price of losing the lines.
	h.autoReconcile(ctx, orgID, conn, res)
	if n, cerr := h.repos.BankFeeds.CountUpstreamChanges(ctx, orgID, conn.ConnectionID); cerr == nil {
		res.upstream = n
	}
	now := time.Now().UTC()
	if err := h.repos.BankFeeds.UpdateConnectionStatus(ctx, orgID, conn.ConnectionID,
		models.BankFeedStatusLinked, "", &now); err != nil {
		return res, err
	}
	return res, nil
}

// recordSyncFailure decides what one failed sync means for the connection.
//
// A provider that says the consent itself is gone leaves the row in ERROR, which
// is what puts Reconnect in front of the user and takes the feed out of the
// background sweep. Anything else — a timeout, a rate limit, a provider outage —
// is a bad sync of a feed that still works: the row stays LINKED, the reason goes
// to last_error so it is not lost, and the next sweep tries again. Marking the
// feed broken over one of those would send the user to repair a consent that was
// never the problem, and it is the more expensive mistake of the two.
func (h *BankFeedHandler) recordSyncFailure(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection, p bankfeed.Provider, syncErr error) {
	if bankfeed.ConsentBroken(p, syncErr) {
		_ = h.repos.BankFeeds.UpdateConnectionStatus(ctx, orgID, conn.ConnectionID,
			models.BankFeedStatusError, syncErr.Error(), nil)
		return
	}
	slog.Warn("bank feed sync failed on a working connection",
		"connectionId", conn.ConnectionID, "provider", conn.Provider, "err", syncErr)
	_ = h.repos.BankFeeds.NoteConnectionWarning(ctx, orgID, conn.ConnectionID, syncErr.Error())
}

// autoReconcile runs the account's automatic pass for every ledger account that
// just received lines — the feed's version of what the import wizard does when
// the setting is on, and the reason a line that agrees with a transaction
// already entered on the account does not have to be matched by hand.
//
// Failures are logged and swallowed: the lines are already stored, and a missed
// match is not worth failing a sync over.
func (h *BankFeedHandler) autoReconcile(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection, res *syncResult) {
	if h.reconciler == nil {
		return
	}
	for accountID := range res.accounts {
		matched, err := h.reconciler.AutoReconcileIfEnabled(ctx, orgID, accountID)
		if err != nil {
			slog.Warn("bank feed sync: auto-reconcile failed",
				"connectionId", conn.ConnectionID, "accountId", accountID, "err", err)
			continue
		}
		res.matched += matched
	}
}

// storeLine stages one provider line and records what it landed on: a new line
// on an account bound to a ledger account is what the automatic pass cares
// about.
func (h *BankFeedHandler) storeLine(ctx context.Context, orgID uuid.UUID, feedAccountID uuid.UUID, ledgerAccountID *uuid.UUID, line *bankfeed.StatementLine, res *syncResult) error {
	inserted, err := h.repos.BankFeeds.UpsertStatementLine(ctx, orgID, feedAccountID,
		lineRow(line, feedAccountID), line.Raw)
	if err != nil {
		return err
	}
	res.fetched++
	if inserted {
		res.newLines++
		if ledgerAccountID != nil {
			res.accounts[*ledgerAccountID] = true
		}
	}
	return nil
}

// syncDateWindow pulls a rolling window for every account on the connection.
// It is the GoCardless path: that API is queried per account per date range and
// has no cross-account cursor.
func (h *BankFeedHandler) syncDateWindow(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection, p bankfeed.Provider, cred bankfeed.Credential, res *syncResult) error {
	to := time.Now().UTC()
	from := to.Add(-h.syncWindow)

	// GetConnection always loads the connection's accounts eagerly, so this is
	// already the complete list.
	for _, a := range conn.Accounts {
		lines, err := p.FetchStatementLines(ctx, cred, a.ExternalAccountID, from, to)
		if err != nil {
			return err
		}
		for i := range lines {
			if err := h.storeLine(ctx, orgID, a.FeedAccountID, a.AccountID, &lines[i], res); err != nil {
				return err
			}
		}
	}
	return nil
}

// syncIncremental pulls everything that changed since the connection's cursor,
// applies what the bank has withdrawn, and then advances the cursor. The cursor
// only moves once the lines it covers are stored, so a crash mid-sync replays
// the batch instead of losing it.
func (h *BankFeedHandler) syncIncremental(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection, p bankfeed.IncrementalProvider, cred bankfeed.Credential, res *syncResult) error {
	lines, removed, nextCursor, err := p.SyncConnection(ctx, cred, conn.SyncCursor)
	if err != nil {
		return err
	}
	// A cursor covers the whole consent, so the lines come back carrying an
	// upstream account id we have to map back to the staged feed account — and
	// to the ledger account it is bound to, which is what the automatic pass
	// works on.
	type target struct {
		feedAccountID   uuid.UUID
		ledgerAccountID *uuid.UUID
	}
	byExternal := make(map[string]target, len(conn.Accounts))
	for _, a := range conn.Accounts {
		byExternal[a.ExternalAccountID] = target{feedAccountID: a.FeedAccountID, ledgerAccountID: a.AccountID}
	}
	for i := range lines {
		t, ok := byExternal[lines[i].ExternalAccountID]
		if !ok {
			// An account we have not staged — granted after finalize, or
			// dropped as a type we do not keep. Skipping beats guessing.
			continue
		}
		if err := h.storeLine(ctx, orgID, t.feedAccountID, t.ledgerAccountID, &lines[i].StatementLine, res); err != nil {
			return err
		}
	}
	// What the bank withdrew. A line still in the inbox goes; one that has been
	// coded stays and is marked instead, because by then it is the books. The
	// split is worth a line in the log and nothing else reports it: a withdrawal
	// that lands on a coded line is the one the user has to hear about, and the
	// standing UpstreamChanges count does not say which run produced it.
	dropped, withdrawn, err := h.repos.BankFeeds.ApplyRemovedLines(ctx, orgID, conn.ConnectionID, removed)
	if err != nil {
		return err
	}
	if dropped > 0 || withdrawn > 0 {
		slog.Info("bank feed sync: the bank withdrew lines",
			"connectionId", conn.ConnectionID, "dropped", dropped, "withdrawn", withdrawn)
	}
	if nextCursor != "" && nextCursor != conn.SyncCursor {
		if err := h.repos.BankFeeds.SetSyncCursor(ctx, orgID, conn.ConnectionID, nextCursor); err != nil {
			return err
		}
	}
	return nil
}

// lineRow maps a provider statement line onto the staging row the reconcile
// inbox reads. The provider's counterparty doubles as the payee: that is the
// column the user codes against, and it is the closest thing to a merchant name
// the aggregators give us.
func lineRow(l *bankfeed.StatementLine, feedAccountID uuid.UUID) *models.BankStatementLine {
	id := feedAccountID
	return &models.BankStatementLine{
		FeedAccountID: &id,
		Source:        models.StatementLineSourceFeed,
		ProviderTxID:  l.ProviderTxID,
		PostedAt:      l.PostedAt,
		Amount:        l.Amount,
		CurrencyCode:  l.CurrencyCode,
		Payee:         l.Counterparty,
		Description:   l.Description,
		Counterparty:  l.Counterparty,
		Reference:     l.Reference,
	}
}

// credentialFor decrypts the connection's stored secret for the duration of a
// single provider call. The plaintext never leaves this scope.
//
// The reference is the durable id once consent has been granted, and the
// session handle before that — the two live in different columns because
// consent overwrites the first, and the first-time finalize is the provider
// being asked about the session it was started with (Plaid's link token,
// GoCardless's requisition id). A repair is the same call against the durable id
// the connection already holds.
func (h *BankFeedHandler) credentialFor(conn *models.BankFeedConnection) (bankfeed.Credential, error) {
	cred := bankfeed.Credential{Reference: firstNonBlank(conn.ExternalReference, conn.SessionRef)}
	if len(conn.ExternalSecret) == 0 {
		return cred, nil
	}
	if h.secrets == nil {
		return cred, bankfeed.ErrNoSecretBox
	}
	secret, err := h.secrets.Open(conn.ExternalSecret)
	if err != nil {
		return cred, err
	}
	cred.Secret = secret
	return cred, nil
}

// sealSecret encrypts a per-connection secret before it is stored. A provider
// that issues none returns nil so the column stays NULL.
func (h *BankFeedHandler) sealSecret(secret string) ([]byte, error) {
	if secret == "" {
		return nil, nil
	}
	if h.secrets == nil {
		return nil, bankfeed.ErrNoSecretBox
	}
	return h.secrets.Seal(secret)
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
		res, err := h.syncConnection(ctx, c.OrganisationID, conn)
		if err != nil {
			slog.Warn("bank feed sweep: sync failed",
				"connectionId", c.ConnectionID, "provider", c.Provider, "err", err)
			failed++
			continue
		}
		synced++
		slog.Info("bank feed sweep: synced",
			"connectionId", c.ConnectionID, "fetched", res.fetched,
			"newLines", res.newLines, "autoMatched", res.matched)
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
