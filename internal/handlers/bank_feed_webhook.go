package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/shurco/goxero/internal/bankfeed"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// plaidWebhookPayload is the union of the deliveries this endpoint acts on.
// Plaid sends one flat object per webhook and which fields are present depends
// on the webhook type, so the ones we do not read simply arrive empty.
type plaidWebhookPayload struct {
	Type         string   `json:"webhook_type"`
	Code         string   `json:"webhook_code"`
	LinkToken    string   `json:"link_token"`
	ItemID       string   `json:"item_id"`
	PublicTokens []string `json:"public_tokens"`
	// Error is only carried by an ITEM: ERROR delivery, and says why the Item
	// stopped working.
	Error *struct {
		Code    string `json:"error_code"`
		Message string `json:"error_message"`
	} `json:"error"`
}

// PlaidWebhook receives Plaid's out-of-band notifications about the bank feed.
//
// It is the one bank feed route without a tenant header — the caller is Plaid,
// not a logged-in user — so it authenticates the caller by verifying Plaid's JWS
// signature rather than a session, and finds the connection from the id in the
// payload instead of from the request. That makes every action it takes
// deliberately small: finish a consent, or record that one stopped working.
//
// A delivery it understood is always answered 200, including one it ignores:
// Plaid retries anything else for up to 24 hours, and there is nothing to gain
// from being asked again about a webhook this integration does not act on. A
// delivery it understood but failed to *act* on answers 500, because that one is
// worth another attempt.
func (h *BankFeedHandler) PlaidWebhook(c fiber.Ctx) error {
	p, err := h.providers.Get(bankfeed.ProviderPlaid)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "plaid is not configured")
	}
	verifier, ok := p.(bankfeed.WebhookVerifier)
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "provider does not sign its webhooks")
	}
	// The signature covers the body byte for byte — Plaid signs a hash of it —
	// so this must be the raw body, read before anything has parsed it.
	body := c.Body()
	if err := verifier.VerifyWebhook(c.Context(), c.Get("Plaid-Verification"), body); err != nil {
		slog.Warn("bank feed webhook: rejected", "err", err)
		// Having refused to check is not the same as having found the signature
		// wrong, and the two want different answers: 401 records a verdict we never
		// reached, while 503 is what Plaid retries.
		if errors.Is(err, bankfeed.ErrWebhookVerifyThrottled) {
			return fiber.NewError(fiber.StatusServiceUnavailable, "webhook verification is temporarily unavailable")
		}
		return fiber.NewError(fiber.StatusUnauthorized, "invalid webhook signature")
	}

	var ev plaidWebhookPayload
	if err := json.Unmarshal(body, &ev); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "webhook payload is not JSON")
	}

	switch {
	case ev.Type == "LINK" && ev.Code == "SESSION_FINISHED":
		return h.onSessionFinished(c, ev)
	case ev.Type == "ITEM":
		return h.onItemEvent(c, ev)
	default:
		// Something Plaid added that we do not model. Acknowledged, so it is not
		// retried for a day.
		slog.Info("bank feed webhook: ignored", "type", ev.Type, "code", ev.Code)
		return c.JSON(fiber.Map{"Received": true, "Ignored": true})
	}
}

// onSessionFinished completes a consent the user has just finished in Plaid's
// Hosted Link, without waiting for the browser to come back to us.
//
// This is the path that still works when the tab is closed before the redirect
// lands, and it is what Plaid documents as the primary way to collect a public
// token. The redirect stays the primary path *here*, because a self-hosted
// goXero often cannot be reached from the internet at all — the two are not
// alternatives, they are the same completion arriving by different routes, and
// whichever gets there first wins.
func (h *BankFeedHandler) onSessionFinished(c fiber.Ctx, ev plaidWebhookPayload) error {
	if ev.LinkToken == "" {
		slog.Info("bank feed webhook: SESSION_FINISHED without a link token")
		return c.JSON(fiber.Map{"Received": true, "Ignored": true})
	}
	orgID, connID, err := h.repos.BankFeeds.ConnectionBySessionRef(c.Context(), ev.LinkToken)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// A session we are not waiting on: already completed by the browser,
			// or started against a connection that has since gone. Either way
			// there is nothing to do, and asking Plaid to try again would not
			// change that.
			slog.Info("bank feed webhook: no connection is waiting on that session")
			return c.JSON(fiber.Map{"Received": true, "Ignored": true})
		}
		return httpError(err)
	}
	conn, err := h.repos.BankFeeds.GetConnection(c.Context(), orgID, connID)
	if err != nil {
		return httpError(err)
	}

	var cerr error
	switch {
	case hasDurableConsent(conn):
		// A repair: the Item already exists and its credential is still the one
		// to use. No public token is exchanged — doing so would create a second
		// Item for an account we already track.
		cerr = h.completeConsent(c.Context(), orgID, conn)
	case len(ev.PublicTokens) > 0:
		cerr = h.completeConsentFromToken(c.Context(), orgID, conn, ev.PublicTokens[0])
	default:
		// The session ended without adding an Item, so the user backed out. The
		// browser path reports the same outcome if it ever arrives.
		slog.Info("bank feed webhook: session finished with no item", "connectionId", connID)
		return c.JSON(fiber.Map{"Received": true, "Ignored": true})
	}
	if cerr != nil {
		slog.Error("bank feed webhook: could not complete consent",
			"connectionId", connID, "err", cerr)
		return cerr
	}
	slog.Info("bank feed webhook: consent completed", "connectionId", connID)
	return c.JSON(fiber.Map{"Received": true})
}

// completeConsentFromToken finishes a first-time consent the provider delivered
// to us directly. The public token is exactly what the browser return would have
// read out of the session, so from here on it is the same code path.
func (h *BankFeedHandler) completeConsentFromToken(ctx context.Context, orgID uuid.UUID, conn *models.BankFeedConnection, publicToken string) error {
	p, err := h.resolveProvider(conn.Provider)
	if err != nil {
		return err
	}
	exchanger, ok := p.(bankfeed.PublicTokenExchanger)
	if !ok {
		return fiber.NewError(fiber.StatusBadRequest, "provider cannot complete a consent out of band")
	}
	consent, err := exchanger.ExchangePublicToken(ctx, publicToken)
	if err != nil {
		_ = h.repos.BankFeeds.FailFinalize(ctx, orgID, conn.ConnectionID, err.Error())
		return httpError(err)
	}
	return h.persistConsent(ctx, orgID, conn, consent)
}

// onItemEvent handles news about an Item we already hold. These arrive on their
// own schedule, long after any consent flow, and they are the only warning we
// get that a feed has stopped working while nobody was looking at it.
//
// Every one of them is matched by item_id: an ITEM webhook is about the
// connection, not about the session that created it.
func (h *BankFeedHandler) onItemEvent(c fiber.Ctx, ev plaidWebhookPayload) error {
	if ev.ItemID == "" {
		slog.Info("bank feed webhook: item webhook without an item id", "code", ev.Code)
		return c.JSON(fiber.Map{"Received": true, "Ignored": true})
	}
	orgID, connID, err := h.repos.BankFeeds.ConnectionByExternalReference(c.Context(), bankfeed.ProviderPlaid, ev.ItemID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			slog.Info("bank feed webhook: item is not one of ours", "code", ev.Code)
			return c.JSON(fiber.Map{"Received": true, "Ignored": true})
		}
		return httpError(err)
	}

	switch ev.Code {
	case "ERROR":
		// Plaid has stopped serving this Item and wants the user to fix it —
		// ITEM_LOGIN_REQUIRED, most often, which is what the Reconnect button on
		// the connections screen exists for. The message is stored rather than
		// the raw code, because the code means nothing to a bookkeeper.
		msg := "the bank stopped accepting this connection"
		if ev.Error != nil {
			msg = firstNonBlank(ev.Error.Code+": "+ev.Error.Message, ev.Error.Message, msg)
		}
		if err := h.repos.BankFeeds.FailLinkedConnection(c.Context(), orgID, connID, msg); err != nil {
			return httpError(err)
		}
		slog.Info("bank feed webhook: connection needs repair", "connectionId", connID)
	case "USER_PERMISSION_REVOKED":
		if err := h.repos.BankFeeds.RevokeConnection(c.Context(), orgID, connID,
			"access was revoked at the bank"); err != nil {
			return httpError(err)
		}
		slog.Info("bank feed webhook: consent revoked at the bank", "connectionId", connID)
	case "PENDING_EXPIRATION", "PENDING_DISCONNECT":
		// Plaid warns a week before consent lapses. The feed keeps working until
		// it does, so the connection stays LINKED and only carries the warning —
		// a successful sync clears it, which is the one weak spot of parking the
		// warning in last_error, and it costs little: by then it is a week old.
		if err := h.repos.BankFeeds.NoteConnectionWarning(c.Context(), orgID, connID,
			"bank consent expires in a week — reconnect to keep this feed running"); err != nil {
			return httpError(err)
		}
		slog.Info("bank feed webhook: consent expiry announced", "connectionId", connID)
	default:
		// NEW_ACCOUNTS_AVAILABLE, LOGIN_REPAIRED and whatever comes next: news,
		// not an instruction.
		slog.Info("bank feed webhook: item event noted", "connectionId", connID, "code", ev.Code)
	}
	return c.JSON(fiber.Map{"Received": true})
}
