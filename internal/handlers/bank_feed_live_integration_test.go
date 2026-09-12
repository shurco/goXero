package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/bankfeed"
	"github.com/shurco/goxero/internal/config"
	"github.com/shurco/goxero/internal/handlers"
	"github.com/shurco/goxero/internal/middleware"
	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
	"github.com/shurco/goxero/internal/testutil"
)

// TestHTTP_BankFeed_LiveSync is the one test that runs the whole product against
// the real Plaid sandbox: the sandbox credentials become an Item, the Item's
// access token is sealed into the same column production uses, and a real
// `POST /sync` pulls real transactions through the adapter, the cipher and the
// staging upsert into an inbox the user would see.
//
// It is opt-in twice over, because it needs the network and credentials:
//
//	PLAID_LIVE=1 PLAID_CLIENT_ID=... PLAID_SECRET=... \
//	  go test ./internal/handlers/ -run TestHTTP_BankFeed_LiveSync -v -timeout 400s
//
// What it cannot cover is the browser consent click itself — that is Plaid's own
// UI. Everything on either side of it is exercised here: the sandbox item is
// minted through the same exchange the webhook and the redirect both use.
func TestHTTP_BankFeed_LiveSync(t *testing.T) {
	if os.Getenv("PLAID_LIVE") != "1" {
		t.Skip("set PLAID_LIVE=1 to run against Plaid's sandbox")
	}
	clientID, secret := os.Getenv("PLAID_CLIENT_ID"), os.Getenv("PLAID_SECRET")
	if clientID == "" || secret == "" {
		t.Skip("PLAID_CLIENT_ID / PLAID_SECRET are not set")
	}
	ctx := context.Background()

	pool := testutil.NewPool(t)
	repos := repository.New(pool)
	cfg := testCfg()

	plaid := bankfeed.NewPlaid(clientID, secret, "goxero", false)
	secrets, err := bankfeed.NewSecretBox("live-test-passphrase")
	require.NoError(t, err)

	feed := handlers.NewBankFeedHandler(repos, registryOf(plaid), secrets, "http://localhost:5173/app/bank-feeds/callback", 0)
	feed.SetAutoReconciler(handlers.NewBankStatementHandler(repos))
	h := liveHarness(t, repos, cfg, feed)

	// Step one is the request the bank feeds screen sends: a Plaid account that
	// refuses pinned institutions used to fail it outright, which is the one bug
	// no fixture could have caught — a stub answers whatever it is asked. It has
	// to come back with somewhere to consent, institution or no institution.
	status, body := h.do(t, http.MethodPost, "/api/v1/bank-feeds/connections", map[string]any{
		"Provider": "plaid", "InstitutionID": "ins_56", "InstitutionName": "Chase", "Country": "US",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		Connections []models.BankFeedConnection
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.Connections, 1)
	conn := created.Connections[0]
	require.NotEmpty(t, conn.AuthURL, "consent has to be reachable even where the pin is refused")
	t.Logf("consent session for %s/%s: %s", conn.InstitutionID, conn.InstitutionName, conn.AuthURL)

	// A public token, as a finished consent would have produced. Plaid's sandbox
	// mints one without a browser; the exchange that follows is the production
	// code path, which is what makes the rest of this test real.
	publicToken := sandboxPublicToken(t, clientID, secret)
	consent, err := plaid.ExchangePublicToken(ctx, publicToken)
	require.NoError(t, err)
	require.NotEmpty(t, consent.Accounts)

	// Stage what consent would have left behind: a LINKED connection holding the
	// sealed access token, its account bound to a ledger account with automatic
	// reconciliation on — the arrangement a user sets up before anything flows.
	ledger := newStatementAccount(t, h)
	require.NoError(t, repos.Accounts.SetAutoReconcile(ctx, seedDemoOrgID, ledger.AccountID, true))

	sealed, err := secrets.Seal(consent.Secret)
	require.NoError(t, err)
	require.NoError(t, repos.BankFeeds.SetConnectionConsent(ctx, seedDemoOrgID, conn.ConnectionID, consent.Reference, sealed))
	// Broken at the bank, which is the state a user is in when the Reconnect
	// button appears — and the state a repair is for. (A LINKED connection has
	// nothing to finalize: FinalizeConnection skips the provider call for it, so
	// staging LINKED here would assert nothing at all.)
	require.NoError(t, repos.BankFeeds.UpdateConnectionStatus(ctx, seedDemoOrgID, conn.ConnectionID,
		models.BankFeedStatusError, "the login details of this item have changed", nil))

	// A repair is the live path that can actually finish: update mode needs no
	// browser, and it re-reads what the Item exposes — including the institution
	// behind it. The connection was created claiming Chase and the item is First
	// Platypus Bank, so this is where the row has to be corrected to the bank that
	// was really connected.
	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/reconnect", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	status, body = h.do(t, http.MethodPost,
		"/api/v1/bank-feeds/connections/"+conn.ConnectionID.String()+"/finalize", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	conns := listConnections(t, h)
	require.Len(t, conns, 1)
	assert.Equal(t, consent.Institution.ID, conns[0].InstitutionID)
	assert.NotEqual(t, "Chase", conns[0].InstitutionName,
		"the row must be re-labelled with the bank the Item is at, not the one we asked for")
	t.Logf("connection re-labelled to %s (%s)", conns[0].InstitutionID, conns[0].InstitutionName)
	// Whatever it says now is what the repair reported, so the assertion above is
	// about the source and not about a hardcoded name.
	assert.NotEmpty(t, conns[0].InstitutionName)
	assert.Equal(t, models.BankFeedStatusLinked, conns[0].Status, "the repair puts the feed back in service")

	staged := 0
	for _, a := range consent.Accounts {
		fa := &models.BankFeedAccount{
			ConnectionID:      conn.ConnectionID,
			ExternalAccountID: a.ExternalID,
			DisplayName:       a.DisplayName,
			CurrencyCode:      a.CurrencyCode,
		}
		require.NoError(t, repos.BankFeeds.UpsertAccount(ctx, seedDemoOrgID, fa))
		if staged == 0 {
			// Only the first account is bound: the others must stay unbound
			// rather than quietly share a ledger account.
			require.NoError(t, repos.BankFeeds.BindAccount(ctx, seedDemoOrgID, fa.FeedAccountID, &ledger.AccountID))
		}
		staged++
	}

	// A freshly minted sandbox Item has no transactions until its initial update
	// lands, so the sync is retried the way the background poller retries it.
	syncPath := "/api/v1/bank-feeds/connections/" + conn.ConnectionID.String() + "/sync"
	deadline := time.Now().Add(150 * time.Second)
	var summary struct{ Fetched, NewLines, AutoMatched, UpstreamChanges int }
	for {
		status, body := h.do(t, http.MethodPost, syncPath, nil, true)
		require.Equal(t, http.StatusOK, status, string(body))
		require.NoError(t, json.Unmarshal(body, &summary))
		if summary.NewLines > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Plaid never served transactions for the sandbox item (last: %+v)", summary)
		}
		time.Sleep(5 * time.Second)
	}
	t.Logf("live sync: fetched=%d new=%d autoMatched=%d upstream=%d",
		summary.Fetched, summary.NewLines, summary.AutoMatched, summary.UpstreamChanges)

	// The lines are in the inbox, read back through the same API the reconcile
	// screen uses — and carrying what the database requires of a feed line.
	first := listLines(t, h, "?bankAccountId="+ledger.AccountID.String()+"&unreconciled=true")
	require.NotEmpty(t, first)
	var negative, positive int
	for _, l := range first {
		assert.NotEmpty(t, l.StatementLineID)
		assert.NotEmpty(t, l.Amount)
		amount, err := decimal.NewFromString(l.Amount)
		require.NoError(t, err)
		if amount.IsNegative() {
			negative++
		} else {
			positive++
		}
	}
	t.Logf("inbox: %d line(s), %d out / %d in", len(first), negative, positive)
	assert.Positive(t, negative, "a sandbox item has debits, and they must arrive negative")

	// Plaid delivers a new Item's history in stages — an initial update first,
	// then the historical one — so a second sync can legitimately add the older
	// batch rather than nothing at all. Waiting for the item to go quiet is what
	// makes the assertion below about duplication instead of about timing.
	inbox := func() []statementLine {
		return listLines(t, h, "?bankAccountId="+ledger.AccountID.String()+"&unreconciled=true")
	}
	quiet := 0
	for i := 0; i < 12 && quiet < 2; i++ {
		status, body := h.do(t, http.MethodPost, syncPath, nil, true)
		require.Equal(t, http.StatusOK, status, string(body))
		require.NoError(t, json.Unmarshal(body, &summary))
		if summary.NewLines == 0 {
			quiet++
		} else {
			quiet = 0
		}
		if quiet < 2 {
			time.Sleep(3 * time.Second)
		}
	}
	require.Equal(t, 2, quiet, "a settled item must stop producing new lines")

	// Re-syncing must not duplicate anything: ProviderTxID is the dedupe key,
	// and this is the only place it is checked against ids Plaid actually
	// issued rather than ones a fixture made up.
	settled := inbox()
	require.NotEmpty(t, settled)
	seen := map[string]bool{}
	for _, l := range settled {
		assert.False(t, seen[l.ProviderTxID], "transaction %s was staged twice", l.ProviderTxID)
		seen[l.ProviderTxID] = true
	}
	assert.Len(t, settled, len(seen))
	t.Logf("settled inbox: %d line(s)", len(settled))

	// `first` was read while Plaid was still delivering, so it is a lower bound;
	// the point of the two quiet syncs is that the inbox has stopped moving.
	assert.GreaterOrEqual(t, len(settled), len(first))
}

// liveHarness mounts the bank feed routes over the real Plaid adapter. It is
// bankFeedHarness without the stub: the provider talks to Plaid itself, and the
// app in front of it is the same one every other bank feed test drives.
func liveHarness(t *testing.T, repos *repository.Repositories, cfg *config.Config, feed *handlers.BankFeedHandler) *appHarness {
	t.Helper()
	tok, err := middleware.IssueToken(cfg.Auth, seedDemoUserID, "demo@example.com")
	require.NoError(t, err)
	return &appHarness{app: bankFeedApp(repos, cfg, feed), cfg: cfg, repos: repos, token: tok}
}

func registryOf(p bankfeed.Provider) *bankfeed.Registry {
	reg := bankfeed.NewRegistry()
	reg.Register(p)
	return reg
}

// sandboxPublicToken asks Plaid for the token a finished consent would have
// produced, so the exchange that follows runs unchanged. This endpoint exists
// only in the sandbox, which is the whole reason it is safe here.
func sandboxPublicToken(t *testing.T, clientID, secret string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"client_id":        clientID,
		"secret":           secret,
		"institution_id":   "ins_109508",
		"initial_products": []string{"transactions"},
	})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, "https://sandbox.plaid.com/sandbox/public_token/create", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "sandbox/public_token/create")
	var out struct {
		PublicToken string `json:"public_token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.PublicToken)
	return out.PublicToken
}
