package bankfeed

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// livePlaid returns a sandbox adapter, or skips the test.
//
// This file is the one place the adapter talks to the real Plaid API rather than
// to an httptest stand-in. It is opt-in twice over — PLAID_LIVE=1 and actual
// sandbox credentials — because it needs the network and nothing else in the
// suite should. What it buys is the only thing a stub cannot give: proof that
// Plaid accepts the request bodies we build, which is where an adapter actually
// breaks. The unit tests prove we read Plaid's replies correctly; these prove
// Plaid reads ours.
//
//	PLAID_LIVE=1 PLAID_CLIENT_ID=... PLAID_SECRET=... \
//	  go test ./internal/bankfeed/ -run TestPlaidLive -v -timeout 300s
func livePlaid(t *testing.T) *Plaid {
	t.Helper()
	if os.Getenv("PLAID_LIVE") != "1" {
		t.Skip("set PLAID_LIVE=1 to run against Plaid's sandbox")
	}
	id, secret := os.Getenv("PLAID_CLIENT_ID"), os.Getenv("PLAID_SECRET")
	if id == "" || secret == "" {
		t.Skip("PLAID_CLIENT_ID / PLAID_SECRET are not set")
	}
	return NewPlaid(id, secret, "goxero", false)
}

// liveSandboxItem mints an Item without a browser. Plaid's sandbox endpoint
// hands back the public token that a finished consent would have produced, so
// everything downstream of consent — exchange, accounts, transactions — is the
// real code path on real data.
func liveSandboxItem(t *testing.T, p *Plaid) Credential {
	t.Helper()
	var out struct {
		PublicToken string `json:"public_token"`
	}
	require.NoError(t, p.do(context.Background(), "/sandbox/public_token/create", map[string]any{
		"institution_id":   "ins_56",
		"initial_products": []string{"transactions"},
	}, &out))
	require.NotEmpty(t, out.PublicToken)

	consent, err := p.ExchangePublicToken(context.Background(), out.PublicToken)
	require.NoError(t, err)
	require.NotEmpty(t, consent.Reference, "the exchange must yield an item_id")
	require.NotEmpty(t, consent.Secret, "the exchange must yield an access token")
	require.NotEmpty(t, consent.Accounts, "the sandbox item must come with accounts")
	return Credential{Reference: consent.Reference, Secret: consent.Secret}
}

// TestPlaidLiveConsentFlow — the two link tokens an installation has to be able
// to create: a first-time consent, and a repair of an Item it already holds.
// Both are single round trips whose bodies Plaid either accepts or rejects, so
// there is nothing here a stub could tell us.
func TestPlaidLiveConsentFlow(t *testing.T) {
	p := livePlaid(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Both catalogue endpoints, because Plaid wants `products` in a different
	// place for each of them and tells you so only at runtime.
	found, err := p.ListInstitutions(ctx, "US", "chase")
	require.NoError(t, err, "/institutions/search")
	require.NotEmpty(t, found, "a US search must find something")
	require.NotEmpty(t, found[0].ID)

	page, err := p.ListInstitutions(ctx, "US", "")
	require.NoError(t, err, "/institutions/get")
	require.NotEmpty(t, page, "an empty query returns the first page of the catalogue")

	// Pinning the institution — what our own picker sends — is refused by this
	// Plaid account for *every* institution id, including ones its own search
	// just returned, while the identical body without the field is accepted: a
	// restriction on the account (an institution has to be enabled for the team),
	// not anything the adapter builds. That is exactly why the adapter falls back
	// to Plaid's own picker instead of failing, and this asserts the fallback
	// rather than the restriction — an account that does allow pinning takes the
	// other branch, and both leave the caller with a working session.
	pinned, err := p.CreateSession(ctx, SessionRequest{
		InstitutionID: "ins_109508", Country: "US", Reference: "live-test",
		RedirectURL: "http://localhost:5173/app/bank-feeds/callback",
	})
	require.NoError(t, err, "a refused institution must fall back to Plaid's picker, not fail")
	require.NotEmpty(t, pinned.AuthURL)
	t.Logf("pinned institution: honoured=%v", pinned.InstitutionPinned)

	// A first-time consent: Hosted Link, no access token, products requested.
	session, err := p.CreateSession(ctx, SessionRequest{
		Country:     "US",
		RedirectURL: "http://localhost:5173/app/bank-feeds/callback",
		Reference:   "live-test",
	})
	require.NoError(t, err)
	require.NotEmpty(t, session.ExternalReference, "the link token is what identifies the session")
	require.Contains(t, session.AuthURL, "https://", "Hosted Link returns a URL for the browser")
	require.NotNil(t, session.ExpiresAt)

	// A repair: update mode, against the Item we already hold and with no
	// `products` at all — Plaid rejects that request if it names any. Note that
	// this is the flow the pinned-institution restriction above does not touch:
	// update mode names no institution, which is why reconnecting a broken feed
	// works even where a first-time link cannot pin one.
	cred := liveSandboxItem(t, p)
	repair, err := p.CreateReconsentSession(ctx, cred, SessionRequest{
		Country:     "US",
		RedirectURL: "http://localhost:5173/app/bank-feeds/callback",
		Reference:   "live-test",
	})
	require.NoError(t, err, "update mode must be accepted for an Item we hold")
	require.NotEmpty(t, repair.AuthURL)

	// Finishing a repair re-reads the accounts and hands back the same token:
	// an Item's access token is not replaced by update mode, which is why
	// nothing here is exchanged.
	reconsent, err := p.CompleteReconsent(ctx, cred)
	require.NoError(t, err)
	require.Equal(t, cred.Reference, reconsent.Reference)
	require.Equal(t, cred.Secret, reconsent.Secret, "the access token must survive a repair")
	require.NotEmpty(t, reconsent.Accounts)
	// The institution behind the Item is what the handler re-labels the
	// connection with — the only source of it when the user picked the bank in
	// Plaid's own picker rather than in our catalogue.
	require.NotEmpty(t, reconsent.Institution.ID, "the Item must report its institution")
	t.Logf("repair reports institution %s (%s)", reconsent.Institution.ID, reconsent.Institution.Name)
}

// TestPlaidLiveStatementLines — the data path: what Plaid actually sends back,
// through the mapper that has to turn it into our shape. The invariants the
// database depends on (a stable id per line, a currency, a signed amount) are
// asserted here on live payloads rather than on fixtures we wrote ourselves.
func TestPlaidLiveStatementLines(t *testing.T) {
	p := livePlaid(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	cred := liveSandboxItem(t, p)

	// A freshly created Item has no transactions yet: Plaid answers NOT_READY,
	// with nothing added and no cursor, until its initial update lands. That is
	// a Plaid fact rather than an adapter one — the first sync simply has
	// nothing to resume from — so the test waits the way the background poller
	// does, by syncing again.
	var (
		lines   []ConnStatementLine
		removed []string
		cursor  string
	)
	deadline := time.Now().Add(120 * time.Second)
	for {
		var err error
		lines, removed, cursor, err = p.SyncConnection(ctx, cred, "")
		require.NoError(t, err)
		if cursor != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Plaid never reported the sandbox item's transactions as ready")
		}
		time.Sleep(3 * time.Second)
	}
	t.Logf("first sync: %d line(s), %d removed", len(lines), len(removed))

	// Resuming from the cursor Plaid just gave us is the whole of incremental
	// sync, so it is the one thing worth proving twice. The cursor itself is
	// opaque and forward-only — Plaid may move it on a sync that delivers nothing,
	// and the sandbox bank keeps minting transactions besides — so what is
	// asserted is that the update already consumed is not served a second time,
	// which is the property the staging upsert depends on.
	again, _, cursor2, err := p.SyncConnection(ctx, cred, cursor)
	require.NoError(t, err, "a cursor from Plaid must be accepted by Plaid")
	require.NotEmpty(t, cursor2, "every sync hands back the position to resume from")
	served := map[string]bool{}
	for _, l := range lines {
		served[l.ProviderTxID] = true
	}
	for _, l := range again {
		require.False(t, served[l.ProviderTxID],
			"transaction %s came back off the cursor that had already delivered it", l.ProviderTxID)
	}
	t.Logf("resumed sync: %d new line(s), cursor advanced=%v", len(again), cursor2 != cursor)

	for _, l := range lines {
		require.NotEmpty(t, l.ExternalAccountID, "a line must name the account it belongs to")
		require.NotEmpty(t, l.ProviderTxID, "ProviderTxID is the dedupe key and cannot be empty")
		require.NotEmpty(t, l.CurrencyCode, "the staging column is NOT NULL")
	}
	if len(lines) > 0 {
		sample := lines[0].StatementLine
		t.Logf("sample: %s %s %s", sample.PostedAt.Format("2006-01-02"), sample.Amount, sample.Counterparty)
	}

	// The date-window path is what GoCardless uses and Plaid still falls back to
	// when a connection has no cursor, so it has to work too.
	accounts, institution, err := p.accounts(ctx, cred.Secret, cred.Reference)
	require.NoError(t, err)
	require.NotEmpty(t, accounts)
	require.NotEmpty(t, institution.ID, "the institution has to come back with the accounts")
	assert.Equal(t, "ins_56", institution.ID)
	to := time.Now().UTC()
	window, err := p.FetchStatementLines(ctx, cred, accounts[0].ExternalID, to.AddDate(0, 0, -90), to)
	require.NoError(t, err)
	t.Logf("date window: %d line(s) over 90 days on %s", len(window), accounts[0].ExternalID)
}

// TestPlaidLiveRevokeConsent — the Item we mint is removed again, and Plaid is
// asked about it afterwards to prove the removal was real rather than merely
// accepted. Worth doing live because the failure mode is silent: a revoke that
// Plaid refuses leaves a working access token behind, and a stub would agree
// with whatever body we sent.
//
//	PLAID_LIVE=1 PLAID_CLIENT_ID=... PLAID_SECRET=... \
//	  go test ./internal/bankfeed/ -run TestPlaidLiveRevokeConsent -v -timeout 120s
func TestPlaidLiveRevokeConsent(t *testing.T) {
	p := livePlaid(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cred := liveSandboxItem(t, p)

	require.NoError(t, p.RevokeConsent(ctx, cred))

	// The access token is dead: the same call that listed the accounts a moment
	// ago is refused now, which is the whole point of removing the Item.
	_, _, err := p.accounts(ctx, cred.Secret, cred.Reference)
	require.Error(t, err, "a removed Item must no longer answer for its accounts")
	var apiErr *PlaidError
	require.ErrorAs(t, err, &apiErr)
	t.Logf("after removal Plaid answers %s: %s", apiErr.Code, apiErr.Message)
	assert.Contains(t, apiErr.Code, "ITEM")

	// A credential with no secret is a consent that never became an Item: there
	// is nothing to remove, and asking Plaid about it would be an error of ours.
	require.NoError(t, p.RevokeConsent(ctx, Credential{Reference: "link-sandbox-whatever"}))
}
