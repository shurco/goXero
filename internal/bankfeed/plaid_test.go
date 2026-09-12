package bankfeed

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/testutil"
)

// TestMapPlaidTx pins the invariants the staging table relies on. The sign flip
// is the load-bearing one: Plaid reports money leaving the account as a positive
// amount, while StatementLine.Amount is positive for money coming in. Getting it
// backwards would invert every reconciliation in the inbox.
func TestMapPlaidTx(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		payload   string
		wantID    string
		wantAmt   string
		wantDate  string
		wantCur   string
		wantDesc  string
		wantParty string
		wantRef   string
	}{
		{
			name: "debit becomes negative",
			payload: `{
				"transaction_id":"tx-debit","account_id":"acc-1",
				"amount":25.50,"iso_currency_code":"USD","date":"2026-04-01",
				"name":"STARBUCKS STORE 1234","merchant_name":"Starbucks"
			}`,
			wantID: "tx-debit", wantAmt: "-25.5", wantDate: "2026-04-01T00:00:00Z",
			wantCur: "USD", wantDesc: "STARBUCKS STORE 1234", wantParty: "Starbucks",
		},
		{
			name: "credit becomes positive",
			payload: `{
				"transaction_id":"tx-credit","account_id":"acc-1",
				"amount":-1500.00,"iso_currency_code":"USD","date":"2026-04-02",
				"name":"ACME PAYROLL"
			}`,
			wantID: "tx-credit", wantAmt: "1500", wantDate: "2026-04-02T00:00:00Z",
			wantCur: "USD", wantDesc: "ACME PAYROLL", wantParty: "ACME PAYROLL",
		},
		{
			name: "null iso currency falls back to USD",
			payload: `{
				"transaction_id":"tx-nocur","account_id":"acc-1",
				"amount":10,"iso_currency_code":null,"date":"2026-04-03",
				"name":"UNKNOWN CURRENCY BANK"
			}`,
			wantID: "tx-nocur", wantAmt: "-10", wantDate: "2026-04-03T00:00:00Z",
			wantCur: "USD", wantDesc: "UNKNOWN CURRENCY BANK", wantParty: "UNKNOWN CURRENCY BANK",
		},
		{
			name: "reference falls back to check number",
			payload: `{
				"transaction_id":"tx-check","account_id":"acc-1",
				"amount":100,"iso_currency_code":"USD","date":"2026-04-04",
				"name":"CHECK 1042","check_number":"1042"
			}`,
			wantID: "tx-check", wantAmt: "-100", wantDate: "2026-04-04T00:00:00Z",
			wantCur: "USD", wantDesc: "CHECK 1042", wantParty: "CHECK 1042", wantRef: "1042",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var tx plaidTx
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &tx))

			line, err := mapPlaidTx(tx)
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, line.ProviderTxID)
			assert.True(t, decimal.RequireFromString(tc.wantAmt).Equal(line.Amount),
				"amount: want %s, got %s", tc.wantAmt, line.Amount)
			assert.Equal(t, tc.wantDate, line.PostedAt.Format("2006-01-02T15:04:05Z"))
			assert.Equal(t, tc.wantCur, line.CurrencyCode)
			assert.Equal(t, tc.wantDesc, line.Description)
			assert.Equal(t, tc.wantParty, line.Counterparty)
			assert.Equal(t, tc.wantRef, line.Reference)
			// The raw payload is kept for audit, so it has to be valid JSON.
			assert.True(t, json.Valid(line.Raw))
		})
	}
}

// TestMapPlaidTxRejectsUnusableRows — a line we cannot dedup or date must fail
// loudly rather than land in the inbox keyed on an empty id.
func TestMapPlaidTxRejectsUnusableRows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, payload string }{
		{"missing transaction id", `{"account_id":"acc-1","amount":5,"date":"2026-04-01","name":"X"}`},
		{"unparseable date", `{"transaction_id":"tx-1","amount":5,"date":"01/04/2026","name":"X"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var tx plaidTx
			require.NoError(t, json.Unmarshal([]byte(tc.payload), &tx))
			_, err := mapPlaidTx(tx)
			require.Error(t, err)
		})
	}
}

// plaidTestServer stands in for the Plaid API. Handlers are keyed by path; every
// request is checked for the client credentials Plaid expects in the body, since
// forgetting them is the easiest way to build an adapter that only fails live.
func plaidTestServer(t *testing.T, routes map[string]func(t *testing.T, body map[string]any) (int, string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "client-id", body["client_id"], "every Plaid call must carry client_id")
		assert.Equal(t, "secret", body["secret"], "every Plaid call must carry secret")

		status, payload := handler(t, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
}

// TestPlaidConsentFlow walks the whole Hosted Link handoff: the link_token we
// stored comes back with a public_token, which is traded for the durable Item,
// and the Item's accounts are what the caller ends up persisting.
func TestPlaidConsentFlow(t *testing.T) {
	t.Parallel()
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/link/token/create": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "ins_56", body["institution_id"])
			assert.Equal(t, []any{"CA"}, body["country_codes"], "the searched country must carry into the link token")
			hosted, ok := body["hosted_link"].(map[string]any)
			require.True(t, ok, "Hosted Link requires a hosted_link object")
			assert.Equal(t, "https://app.example/app/bank-feeds/callback", hosted["completion_redirect_uri"])
			return 200, `{"link_token":"link-sandbox-1","hosted_link_url":"https://secure.plaid.com/hl/abc","expiration":"2026-04-01T12:00:00Z"}`
		},
		"/link/token/get": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "link-sandbox-1", body["link_token"])
			return 200, `{"link_sessions":[{"results":{"item_add_results":[{"public_token":"public-sandbox-9"}]}}]}`
		},
		"/item/public_token/exchange": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "public-sandbox-9", body["public_token"])
			return 200, `{"access_token":"access-sandbox-7","item_id":"item-42"}`
		},
		"/accounts/get": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "access-sandbox-7", body["access_token"])
			return 200, `{"accounts":[
				{"account_id":"acc-chk","name":"Plaid Checking","official_name":"Plaid Gold Standard 0% Interest Checking",
				 "mask":"0000","type":"depository","subtype":"checking",
				 "balances":{"current":110.00,"iso_currency_code":"USD"}},
				{"account_id":"acc-inv","name":"Plaid IRA","type":"investment","subtype":"ira",
				 "balances":{"current":320.76,"iso_currency_code":"USD"}}
			]}`
		},
	})
	defer srv.Close()

	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	session, err := p.CreateSession(context.Background(), SessionRequest{
		InstitutionID: "ins_56",
		Country:       "ca",
		RedirectURL:   "https://app.example/app/bank-feeds/callback",
		Reference:     "conn-uuid",
	})
	require.NoError(t, err)
	assert.Equal(t, "link-sandbox-1", session.ExternalReference)
	assert.Equal(t, "https://secure.plaid.com/hl/abc", session.AuthURL)
	require.NotNil(t, session.ExpiresAt)
	assert.True(t, session.InstitutionPinned, "the institution we asked for is the one the session opens on")

	consent, err := p.FinalizeSession(context.Background(), Credential{Reference: session.ExternalReference})
	require.NoError(t, err)
	// The durable identity replaces the throwaway link_token, and the access
	// token is what the handler seals before storing.
	assert.Equal(t, "item-42", consent.Reference)
	assert.Equal(t, "access-sandbox-7", consent.Secret)
	require.Len(t, consent.Accounts, 1, "investment accounts are not bank feeds")
	assert.Equal(t, "acc-chk", consent.Accounts[0].ExternalID)
	assert.Equal(t, "Plaid Gold Standard 0% Interest Checking ••0000", consent.Accounts[0].DisplayName)
	assert.Equal(t, "USD", consent.Accounts[0].CurrencyCode)
	require.NotNil(t, consent.Accounts[0].Balance)
	assert.True(t, decimal.NewFromInt(110).Equal(*consent.Accounts[0].Balance))
}

// --- a pinned institution the account will not accept ------------------------

// TestPlaidCreateSessionFallsBackToPlaidPicker — some Plaid accounts are not
// allowed to pin an institution, and refuse every institution_id with
// INVALID_INSTITUTION while accepting the same body without it. That is a
// property of the account, so the adapter starts the session again on Plaid's own
// picker instead of failing: a bank the user has to choose in Link's UI is a
// working connection, and an outright error is not.
func TestPlaidCreateSessionFallsBackToPlaidPicker(t *testing.T) {
	t.Parallel()
	var bodies []map[string]any
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/link/token/create": func(t *testing.T, body map[string]any) (int, string) {
			bodies = append(bodies, body)
			if _, pinned := body["institution_id"]; pinned {
				return 400, `{"error_type":"INVALID_INPUT","error_code":"INVALID_INSTITUTION",` +
					`"error_message":"invalid institution_id provided"}`
			}
			return 200, `{"link_token":"link-sandbox-2","hosted_link_url":"https://secure.plaid.com/hl/pick",` +
				`"expiration":"2026-04-01T12:00:00Z"}`
		},
	})
	defer srv.Close()

	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))
	session, err := p.CreateSession(context.Background(), SessionRequest{
		InstitutionID: "ins_56", Country: "US", Reference: "conn-uuid",
	})
	require.NoError(t, err, "the institution the account refuses must not fail the whole flow")
	require.Len(t, bodies, 2, "the session is started a second time, without the institution")
	assert.Equal(t, "ins_56", bodies[0]["institution_id"])
	assert.NotContains(t, bodies[1], "institution_id", "the fallback drops the field, nothing else")
	assert.False(t, session.InstitutionPinned, "the user chooses inside Plaid's own picker")
	assert.Equal(t, "https://secure.plaid.com/hl/pick", session.AuthURL)
	// Everything else about the request is the same one, so the retry cannot
	// quietly open a different flow.
	assert.Equal(t, "conn-uuid", bodies[1]["user"].(map[string]any)["client_user_id"])
	assert.Contains(t, bodies[1], "products")
}

// TestPlaidCreateSessionRefusesToSwallowOtherErrors — the fallback is for one
// error code. A rejected product or a bad redirect URI has to reach the caller,
// because retrying it without the institution would open a flow nobody asked for.
func TestPlaidCreateSessionRefusesToSwallowOtherErrors(t *testing.T) {
	t.Parallel()
	calls := 0
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/link/token/create": func(*testing.T, map[string]any) (int, string) {
			calls++
			return 400, `{"error_type":"INVALID_REQUEST","error_code":"INVALID_FIELD",` +
				`"error_message":"hosted_link url_lifetime_seconds is invalid"}`
		},
	})
	defer srv.Close()

	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))
	_, err := p.CreateSession(context.Background(), SessionRequest{InstitutionID: "ins_56", Country: "US"})
	require.Error(t, err)
	assert.Equal(t, 1, calls, "only INVALID_INSTITUTION earns a retry")
	var apiErr *PlaidError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "INVALID_FIELD", apiErr.Code)
}

// TestPlaidFinalizeBeforeUserFinishes — clicking "finalize" on a session the user
// abandoned is a normal mistake, not a server error, so it must surface as a
// plain error the handler can show.
func TestPlaidFinalizeBeforeUserFinishes(t *testing.T) {
	t.Parallel()
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/link/token/get": func(*testing.T, map[string]any) (int, string) {
			return 200, `{"link_sessions":[{"results":{"item_add_results":[]}}]}`
		},
	})
	defer srv.Close()

	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))
	_, err := p.FinalizeSession(context.Background(), Credential{Reference: "link-sandbox-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not completed")
	assert.ErrorIs(t, err, ErrConsentNotFinished,
		"the handler answers 400 off this sentinel; a bare error would be masked as a 500")
}

// TestPlaidSyncConnection covers the cursor contract: every page is followed
// until has_more clears, deleted transactions are reported so the handler can
// drop their staging rows, and the final cursor is what the connection stores.
func TestPlaidSyncConnection(t *testing.T) {
	t.Parallel()
	calls := 0
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/transactions/sync": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "access-sandbox-7", body["access_token"])
			calls++
			switch calls {
			case 1:
				assert.Equal(t, "", body["cursor"], "the first sync starts from an empty cursor")
				return 200, `{
					"added":[
						{"transaction_id":"tx-1","account_id":"acc-chk","amount":12.34,
						 "iso_currency_code":"USD","date":"2026-04-01","name":"COFFEE","pending":true}
					],
					"modified":[],
					"removed":[{"transaction_id":"tx-stale"}],
					"next_cursor":"cursor-1","has_more":true
				}`
			default:
				// A pending charge that posted: the old id is withdrawn and the
				// settled one arrives, under a new amount.
				assert.Equal(t, "cursor-1", body["cursor"], "the second page resumes from the first cursor")
				return 200, `{
					"added":[
						{"transaction_id":"tx-2","account_id":"acc-chk","amount":-250.00,
						 "iso_currency_code":"USD","date":"2026-04-02","name":"PAYROLL","merchant_name":"Acme Inc"}
					],
					"modified":[
						{"transaction_id":"tx-1","account_id":"acc-chk","amount":12.34,
						 "iso_currency_code":"USD","date":"2026-04-02","name":"COFFEE SETTLED"}
					],
					"removed":[{"transaction_id":"tx-1-pending"}],
					"next_cursor":"cursor-2","has_more":false
				}`
			}
		},
	})
	defer srv.Close()

	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	lines, removed, next, err := p.SyncConnection(context.Background(), Credential{
		Reference: "item-42", Secret: "access-sandbox-7",
	}, "")
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "cursor-2", next)
	assert.Equal(t, []string{"tx-stale", "tx-1-pending"}, removed)

	require.Len(t, lines, 3, "added and modified both feed the inbox")
	for _, l := range lines {
		assert.Equal(t, "acc-chk", l.ExternalAccountID, "each line names its upstream account")
	}
	assert.Equal(t, "tx-1", lines[0].ProviderTxID)
	assert.True(t, decimal.RequireFromString("-12.34").Equal(lines[0].Amount))
	assert.Equal(t, "tx-2", lines[1].ProviderTxID)
	assert.True(t, decimal.RequireFromString("250").Equal(lines[1].Amount), "a debit is negative in our convention")
	assert.Equal(t, "Acme Inc", lines[1].Counterparty)
	// The modified copy of tx-1 arrives last, so its settled description is the
	// one the staging upsert leaves in place.
	assert.Equal(t, "tx-1", lines[2].ProviderTxID)
	assert.Equal(t, "COFFEE SETTLED", lines[2].Description)
}

// TestPlaidSyncRequiresToken — a connection whose secret failed to store must not
// be synced with an empty bearer token.
func TestPlaidSyncRequiresToken(t *testing.T) {
	t.Parallel()
	p := NewPlaid("client-id", "secret", "goxero", false)
	_, _, _, err := p.SyncConnection(context.Background(), Credential{Reference: "item-42"}, "")
	require.Error(t, err)
	_, err = p.FetchStatementLines(context.Background(), Credential{Reference: "item-42"}, "acc-1", time.Now(), time.Now())
	require.Error(t, err)
}

// TestPlaidInstitutionsQuadrature — an empty query lists the catalogue head,
// while a query is answered by Plaid's own search index (the US catalogue is far
// too large to enumerate).
func TestPlaidInstitutionsQuadrature(t *testing.T) {
	t.Parallel()
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/institutions/get": func(t *testing.T, body map[string]any) (int, string) {
			assert.EqualValues(t, 500, body["count"])
			assert.Equal(t, []any{"US"}, body["country_codes"])
			return 200, `{"institutions":[{"institution_id":"ins_1","name":"Chase","logo":"https://logo/1.png","country_codes":["US"]}]}`
		},
		"/institutions/search": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "chase", body["query"])
			return 200, `{"institutions":[{"institution_id":"ins_1","name":"Chase","logo":null,"country_codes":["US"]}]}`
		},
	})
	defer srv.Close()

	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	all, err := p.ListInstitutions(context.Background(), "us", "")
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "https://logo/1.png", all[0].LogoURL)
	assert.Equal(t, 730, all[0].TxDays)

	found, err := p.ListInstitutions(context.Background(), "us", "chase")
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "Chase", found[0].Name)
	assert.Empty(t, found[0].LogoURL, "a null logo must not become the string \"null\"")
}

// TestPlaidErrorCarriesCode — the handler records this on the connection, so the
// error has to name the Plaid code a user can act on (re-authenticate, retry).
func TestPlaidErrorCarriesCode(t *testing.T) {
	t.Parallel()
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/accounts/get": func(*testing.T, map[string]any) (int, string) {
			return 400, `{"error_type":"ITEM_ERROR","error_code":"ITEM_LOGIN_REQUIRED",
				"error_message":"the login details of this item have changed",
				"display_message":"Please update your credentials"}`
		},
	})
	defer srv.Close()

	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	_, _, err := p.accounts(context.Background(), "access-sandbox-7", "item-42")
	require.Error(t, err)
	var plaidErr *PlaidError
	require.ErrorAs(t, err, &plaidErr)
	assert.Equal(t, "ITEM_LOGIN_REQUIRED", plaidErr.Code)
	assert.Equal(t, 400, plaidErr.StatusCode)
	assert.Contains(t, err.Error(), "Please update your credentials")
}

// --- update mode (a repair of an Item we already hold) -----------------------

// TestPlaidReconsentSession pins what makes a repair a repair: the session is
// opened against the access token we already hold, and it carries no products —
// Plaid's rule for update mode, where consent is renewed for what the Item has
// rather than extended with something new. Completing it re-reads the accounts
// and hands back the same credential, because an Item's access token does not
// change when Link is used in update mode and exchanging a fresh public token
// would create a second Item for an account we already track.
func TestPlaidReconsentSession(t *testing.T) {
	t.Parallel()
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/link/token/create": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "access-sandbox-7", body["access_token"])
			assert.NotContains(t, body, "products", "update mode must not ask for products")
			assert.Equal(t, []any{"US"}, body["country_codes"])
			assert.Equal(t, "conn-uuid", body["user"].(map[string]any)["client_user_id"])
			assert.NotContains(t, body, "institution_id", "the Item already knows its institution")
			hosted, ok := body["hosted_link"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "https://app.example/app/bank-feeds/callback", hosted["completion_redirect_uri"])
			return 200, `{"link_token":"link-sandbox-2","hosted_link_url":"https://secure.plaid.com/hl/repair","expiration":"2026-04-01T12:00:00Z"}`
		},
		"/accounts/get": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "access-sandbox-7", body["access_token"])
			return 200, `{"accounts":[
				{"account_id":"acc-chk","name":"Plaid Checking","type":"depository","subtype":"checking",
				 "balances":{"current":42.00,"iso_currency_code":"USD"}}
			]}`
		},
		"/item/public_token/exchange": func(t *testing.T, _ map[string]any) (int, string) {
			t.Error("a repair must not exchange a public token: that would create a second Item")
			return 500, `{}`
		},
	})
	defer srv.Close()

	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))
	cred := Credential{Reference: "item-42", Secret: "access-sandbox-7"}

	session, err := p.CreateReconsentSession(context.Background(), cred, SessionRequest{
		Country:     "us",
		RedirectURL: "https://app.example/app/bank-feeds/callback",
		Reference:   "conn-uuid",
	})
	require.NoError(t, err)
	assert.Equal(t, "link-sandbox-2", session.ExternalReference)
	assert.Equal(t, "https://secure.plaid.com/hl/repair", session.AuthURL)

	consent, err := p.CompleteReconsent(context.Background(), cred)
	require.NoError(t, err)
	assert.Equal(t, "item-42", consent.Reference)
	assert.Equal(t, "access-sandbox-7", consent.Secret, "the credential survives the repair untouched")
	require.Len(t, consent.Accounts, 1)
	assert.Equal(t, "acc-chk", consent.Accounts[0].ExternalID)

	// A connection whose secret never made it to the database cannot be repaired.
	_, err = p.CreateReconsentSession(context.Background(), Credential{Reference: "item-42"}, SessionRequest{})
	require.Error(t, err)
	_, err = p.CompleteReconsent(context.Background(), Credential{Reference: "item-42"})
	require.Error(t, err)
}

// TestPlaidExchangePublicToken — the path the webhook takes: Plaid hands us the
// public token directly, so there is no session to read it out of.
func TestPlaidExchangePublicToken(t *testing.T) {
	t.Parallel()
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/item/public_token/exchange": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "public-sandbox-9", body["public_token"])
			return 200, `{"access_token":"access-sandbox-7","item_id":"item-42"}`
		},
		"/accounts/get": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "access-sandbox-7", body["access_token"])
			return 200, `{"accounts":[
				{"account_id":"acc-chk","name":"Checking","type":"depository",
				 "balances":{"current":10.00,"iso_currency_code":"USD"}}
			],"item":{"institution_id":"ins_56","institution_name":"Chase"}}`
		},
	})
	defer srv.Close()

	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))
	consent, err := p.ExchangePublicToken(context.Background(), "public-sandbox-9")
	require.NoError(t, err)
	assert.Equal(t, "item-42", consent.Reference)
	assert.Equal(t, "access-sandbox-7", consent.Secret)
	require.Len(t, consent.Accounts, 1)
	// The Item is the authority on which bank was connected — the request that
	// started the session only says which one the user asked for.
	assert.Equal(t, "ins_56", consent.Institution.ID)
	assert.Equal(t, "Chase", consent.Institution.Name)

	_, err = p.ExchangePublicToken(context.Background(), "")
	require.Error(t, err, "an empty token is a bug on our side, not a Plaid call")
}

// TestPlaidRevokeConsent — disconnecting a bank in goXero has to end the Item at
// Plaid, because nothing else does: the access token we have just stopped using
// keeps working, and the Item keeps taking up one of the account's limited
// number of them.
func TestPlaidRevokeConsent(t *testing.T) {
	t.Parallel()
	p, tr := flakyPlaid(t, 0, map[string]func(*testing.T, map[string]any) (int, string){
		"/item/remove": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "access-sandbox-7", body["access_token"])
			return 200, `{"request_id":"req-1"}`
		},
	})

	// A consent that never completed has no Item behind it, so there is nothing
	// to remove and nothing to ask Plaid about.
	require.NoError(t, p.RevokeConsent(context.Background(), Credential{Reference: "link-sandbox-1"}))
	assert.Zero(t, tr.calls, "a session that never became an Item is not a Plaid call")

	require.NoError(t, p.RevokeConsent(context.Background(), Credential{Reference: "item-42", Secret: "access-sandbox-7"}))
	assert.Equal(t, 1, tr.calls)

	// Removing an Item cannot be repeated any more than creating one can: a
	// second attempt answers about an Item that is already gone, and the user
	// would be told their disconnect failed when it had in fact worked.
	p, tr = flakyPlaid(t, 1, map[string]func(*testing.T, map[string]any) (int, string){
		"/item/remove": func(*testing.T, map[string]any) (int, string) {
			return 200, `{"request_id":"req-1"}`
		},
	})
	err := p.RevokeConsent(context.Background(), Credential{Reference: "item-42", Secret: "access-sandbox-7"})
	require.Error(t, err, "a stall on the wire is still a failure the caller has to hear about")
	assert.Equal(t, 1, tr.calls, "and it is not retried")
}

// TestPlaidRevokeConsentSurfacesRefusal — Plaid saying no is an answer, and the
// caller logs it rather than pretending the bank was told.
func TestPlaidRevokeConsentSurfacesRefusal(t *testing.T) {
	t.Parallel()
	p, _ := flakyPlaid(t, 0, map[string]func(*testing.T, map[string]any) (int, string){
		"/item/remove": func(*testing.T, map[string]any) (int, string) {
			return 400, `{"error_type":"INVALID_INPUT","error_code":"INVALID_ACCESS_TOKEN",
				"error_message":"provided access token is not valid"}`
		},
	})
	err := p.RevokeConsent(context.Background(), Credential{Reference: "item-42", Secret: "access-sandbox-7"})
	require.Error(t, err)
	var apiErr *PlaidError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "INVALID_ACCESS_TOKEN", apiErr.Code)
}

// --- webhook verification ----------------------------------------------------

// TestPlaidVerifyWebhook is the contract of the one public route in the bank
// feed: a delivery that is not signed by Plaid must not be allowed to change
// anything, and the check has to cover the body as well as the sender — a
// signature that is valid but was made over a different payload is exactly how a
// stranger would mark somebody's connection linked.
func TestPlaidVerifyWebhook(t *testing.T) {
	t.Parallel()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	fetches := 0
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/webhook_verification_key/get": func(t *testing.T, body map[string]any) (int, string) {
			assert.Equal(t, "kid-1", body["key_id"])
			fetches++
			return 200, testutil.PlaidJWK(t, &key.PublicKey, "kid-1")
		},
	})
	defer srv.Close()
	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	body := []byte(`{"webhook_type":"LINK","webhook_code":"SESSION_FINISHED","link_token":"link-sandbox-1"}`)
	now := time.Now()
	ctx := context.Background()

	require.NoError(t, p.VerifyWebhook(ctx, testutil.SignPlaidWebhook(t, key, "kid-1", body, now, "ES256"), body))
	assert.Equal(t, 1, fetches, "the signing key is fetched once and then cached")

	require.NoError(t, p.VerifyWebhook(ctx, testutil.SignPlaidWebhook(t, key, "kid-1", body, now, "ES256"), body))
	assert.Equal(t, 1, fetches, "a second delivery must not cost another round trip")

	for _, tc := range []struct {
		name   string
		header string
		body   []byte
		want   string
	}{
		{
			name:   "body swapped after signing",
			header: testutil.SignPlaidWebhook(t, key, "kid-1", body, now, "ES256"),
			body:   []byte(`{"webhook_type":"LINK","webhook_code":"SESSION_FINISHED","link_token":"link-sandbox-evil"}`),
			want:   "signed digest",
		},
		{
			name:   "signed by somebody else",
			header: testutil.SignPlaidWebhook(t, other, "kid-1", body, now, "ES256"),
			body:   body,
			want:   "signature",
		},
		{
			name:   "replayed from an hour ago",
			header: testutil.SignPlaidWebhook(t, key, "kid-1", body, now.Add(-time.Hour), "ES256"),
			body:   body,
			want:   "signed",
		},
		{
			name:   "not ES256",
			header: testutil.SignPlaidWebhook(t, key, "kid-1", body, now, "HS256"),
			body:   body,
			want:   "ES256",
		},
		{name: "no header", header: "", body: body, want: "Plaid-Verification"},
		{name: "not a JWT", header: "nonsense", body: body, want: "signed token"},
		{name: "empty", header: "", body: nil, want: "Plaid-Verification"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := p.VerifyWebhook(ctx, tc.header, tc.body)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestPlaidWebhookKeyCacheIsBounded — the endpoint is public, so a stranger who
// posts a made-up `kid` must not be able to make us call Plaid once per request
// for as long as they keep asking. This is the case a cache of successes cannot
// catch: an unknown key never succeeds, so it never fills the cache and the cap
// never says anything about it. What bounds the calls is remembering the refusal.
func TestPlaidWebhookKeyCacheIsBounded(t *testing.T) {
	t.Parallel()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	fetches := 0
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/webhook_verification_key/get": func(t *testing.T, body map[string]any) (int, string) {
			fetches++
			if body["key_id"] != "kid-1" {
				return 400, `{"error_type":"INVALID_INPUT","error_code":"INVALID_FIELD",` +
					`"error_message":"no such key"}`
			}
			return 200, testutil.PlaidJWK(t, &key.PublicKey, "kid-1")
		},
	})
	defer srv.Close()
	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	body := []byte(`{}`)
	ctx := context.Background()

	madeUp := testutil.SignPlaidWebhook(t, key, "kid-made-up", body, time.Now(), "ES256")
	for i := 0; i < 5; i++ {
		require.Error(t, p.VerifyWebhook(ctx, madeUp, body))
	}
	assert.Equal(t, 1, fetches, "the refusal is remembered, so asking again costs no round trip")

	// A key Plaid really signs with is unaffected by any of that, and is looked
	// up exactly once however many deliveries name it.
	real := testutil.SignPlaidWebhook(t, key, "kid-1", body, time.Now(), "ES256")
	for i := 0; i < 5; i++ {
		require.NoError(t, p.VerifyWebhook(ctx, real, body))
	}
	assert.Equal(t, 2, fetches)
}

// TestPlaidRefusedKey — only Plaid's own answer is worth remembering. A failure
// to reach Plaid says nothing about the key, and a cache that confused the two
// would let a moment's network trouble refuse every delivery for an hour.
func TestPlaidRefusedKey(t *testing.T) {
	t.Parallel()
	assert.True(t, plaidRefusedKey(&PlaidError{StatusCode: http.StatusBadRequest, Code: "INVALID_FIELD"}))
	assert.True(t, plaidRefusedKey(&PlaidError{StatusCode: http.StatusNotFound}))
	assert.False(t, plaidRefusedKey(&PlaidError{StatusCode: http.StatusInternalServerError}),
		"an outage is not an answer about the key")
	assert.False(t, plaidRefusedKey(errors.New("dial tcp: i/o timeout")),
		"nor is failing to reach Plaid at all")
	assert.False(t, plaidRefusedKey(nil))
}

// TestPlaidWebhookKeyCacheDoesNotRememberOutages — the same rule at the cache:
// while Plaid is unreachable, every delivery must try again rather than inherit
// the outage from the one before it.
func TestPlaidWebhookKeyCacheDoesNotRememberOutages(t *testing.T) {
	t.Parallel()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	var fetches atomic.Int32
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/webhook_verification_key/get": func(t *testing.T, body map[string]any) (int, string) {
			fetches.Add(1)
			return http.StatusInternalServerError, `{"error_type":"API_ERROR","error_code":"INTERNAL_SERVER_ERROR"}`
		},
	})
	defer srv.Close()
	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	body := []byte(`{}`)
	ctx := context.Background()
	token := testutil.SignPlaidWebhook(t, key, "kid-1", body, time.Now(), "ES256")
	for i := 0; i < 2; i++ {
		require.Error(t, p.VerifyWebhook(ctx, token, body))
	}
	// Two deliveries, two lookups. The retry inside do() does not double these:
	// it covers a failure on the wire, and a 500 is an answer, however unhelpful.
	assert.Equal(t, int32(2), fetches.Load(),
		"each delivery must ask again; caching an outage would make the second free")
}

// TestPlaidWebhookKeyCacheEvictsRatherThanRefuses — a cache that is full must
// still accept a key it has not seen. Refusing once full means a process that has
// been asked about enough keys stops honouring deliveries altogether, and only a
// restart heals it, which is a far worse failure than one extra round trip.
func TestPlaidWebhookKeyCacheEvictsRatherThanRefuses(t *testing.T) {
	t.Parallel()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/webhook_verification_key/get": func(t *testing.T, body map[string]any) (int, string) {
			if body["key_id"] != "kid-real" {
				return 400, `{"error_type":"INVALID_INPUT","error_code":"INVALID_FIELD",` +
					`"error_message":"no such key"}`
			}
			return 200, testutil.PlaidJWK(t, &key.PublicKey, "kid-real")
		},
	})
	defer srv.Close()
	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	body := []byte(`{}`)
	ctx := context.Background()
	// Fill the cache directly rather than with 32 round trips. The subject here is
	// what an insert does when there is no room, and filling it through refusals
	// would be spending the separate unknown-key budget at the same time — two
	// mechanisms under test at once, one of them incidentally. These entries stand
	// in for the junk an attacker naming invented keys leaves behind.
	for i := 0; i < plaidKeyCacheMax; i++ {
		p.keys[fmt.Sprintf("kid-%d", i)] = plaidWebhookKey{
			err:       errors.New("no such key"),
			expiresAt: time.Now().Add(time.Hour),
		}
	}

	require.NoError(t, p.VerifyWebhook(ctx,
		testutil.SignPlaidWebhook(t, key, "kid-real", body, time.Now(), "ES256"), body),
		"a full cache must not stop a real delivery from being verified")

	// And it was kept, not merely answered: an insert that quietly gave up would
	// leave the key unknown for the rest of the process, so every delivery naming
	// it would cost another round trip.
	_, cached := p.cachedWebhookKey("kid-real")
	assert.True(t, cached, "the new key is stored, making room by evicting one")
	assert.Len(t, p.keys, plaidKeyCacheMax, "and the cache stays bounded")
}

// TestPlaidWebhookKeyLookupsAreRateLimited — the cache bounds one invented `kid`
// however often it is repeated, but a stream of *different* invented ids is a
// fresh miss every time, and every miss is a call to Plaid carrying our client
// credentials. The endpoint is public, so what such a stream can cost is bounded
// by rate rather than by key.
func TestPlaidWebhookKeyLookupsAreRateLimited(t *testing.T) {
	t.Parallel()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	fetches := 0
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/webhook_verification_key/get": func(*testing.T, map[string]any) (int, string) {
			fetches++
			return 400, `{"error_type":"INVALID_INPUT","error_code":"INVALID_FIELD","error_message":"no such key"}`
		},
	})
	defer srv.Close()
	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	body := []byte(`{}`)
	ctx := context.Background()
	delivery := func(kid string) string {
		return testutil.SignPlaidWebhook(t, key, kid, body, time.Now(), "ES256")
	}

	for i := 0; i < plaidKeyLookupBurst; i++ {
		require.Error(t, p.VerifyWebhook(ctx, delivery(fmt.Sprintf("kid-%d", i)), body))
	}
	assert.Equal(t, plaidKeyLookupBurst, fetches, "the budget is spent on the misses")

	err = p.VerifyWebhook(ctx, delivery("kid-one-too-many"), body)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWebhookVerifyThrottled,
		"a refused lookup has to be recognisable as ours, not as a verdict on the delivery")
	assert.Equal(t, plaidKeyLookupBurst, fetches, "and no further call is made")

	// The window is what makes this a rate rather than a lifetime cap. A cap that
	// never refilled would leave the endpoint deaf for good — the same failure the
	// cache is built to avoid.
	p.keyMu.Lock()
	p.keyLookupStart = time.Now().Add(-plaidKeyLookupWindow)
	p.keyMu.Unlock()
	require.Error(t, p.VerifyWebhook(ctx, delivery("kid-after-the-window"), body))
	assert.Equal(t, plaidKeyLookupBurst+1, fetches, "a new window restores the budget")
}

// TestPlaidWebhookKeyBudgetDoesNotBlockKnownKeys is the safety property of that
// budget: it is spent looking keys up, never on keys already held. A genuine
// delivery has to survive a flood of invented ids, or the defence against abuse
// becomes a way to lose real notifications.
func TestPlaidWebhookKeyBudgetDoesNotBlockKnownKeys(t *testing.T) {
	t.Parallel()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/webhook_verification_key/get": func(t *testing.T, body map[string]any) (int, string) {
			if body["key_id"] != "kid-real" {
				return 400, `{"error_type":"INVALID_INPUT","error_code":"INVALID_FIELD","error_message":"no such key"}`
			}
			return 200, testutil.PlaidJWK(t, &key.PublicKey, "kid-real")
		},
	})
	defer srv.Close()
	p := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))

	body := []byte(`{}`)
	ctx := context.Background()
	real := testutil.SignPlaidWebhook(t, key, "kid-real", body, time.Now(), "ES256")
	require.NoError(t, p.VerifyWebhook(ctx, real, body))

	var last error
	for i := 0; i < plaidKeyLookupBurst*2; i++ {
		last = p.VerifyWebhook(ctx,
			testutil.SignPlaidWebhook(t, key, fmt.Sprintf("kid-%d", i), body, time.Now(), "ES256"), body)
		require.Error(t, last)
	}
	require.ErrorIs(t, last, ErrWebhookVerifyThrottled, "the flood really did exhaust the budget")

	require.NoError(t, p.VerifyWebhook(ctx, real, body),
		"a delivery naming a key we hold is verified whatever the budget has left")
}

// TestPlaidWebhookURLIsOfferedToPlaid — the webhook is only delivered if every
// link token we create names it. A deployment with no public URL must not send
// one at all, since Plaid would then have nowhere to post.
func TestPlaidWebhookURLIsOfferedToPlaid(t *testing.T) {
	t.Parallel()
	seen := make(chan any, 2)
	srv := plaidTestServer(t, map[string]func(*testing.T, map[string]any) (int, string){
		"/link/token/create": func(t *testing.T, body map[string]any) (int, string) {
			seen <- body["webhook"]
			return 200, `{"link_token":"link-1","hosted_link_url":"https://secure.plaid.com/hl/x"}`
		},
	})
	defer srv.Close()

	withHook := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()),
		WithPlaidWebhookURL("https://goxero.example/api/v1/bank-feeds/webhooks/plaid"))
	_, err := withHook.CreateSession(context.Background(), SessionRequest{Reference: "conn"})
	require.NoError(t, err)
	assert.Equal(t, "https://goxero.example/api/v1/bank-feeds/webhooks/plaid", <-seen)

	// A repair names it too, or a broken consent the user fixes would never
	// report back.
	_, err = withHook.CreateReconsentSession(context.Background(),
		Credential{Reference: "item-42", Secret: "access-7"}, SessionRequest{Reference: "conn"})
	require.NoError(t, err)
	assert.Equal(t, "https://goxero.example/api/v1/bank-feeds/webhooks/plaid", <-seen)

	withoutHook := NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(srv.Client()))
	_, err = withoutHook.CreateSession(context.Background(), SessionRequest{Reference: "conn"})
	require.NoError(t, err)
	assert.Nil(t, <-seen, "no webhook URL means no webhook field")
}

// flakyTransport fails the first `failures` round trips the way a flow held by a
// local content filter or proxy does: no response ever arrives, and Go reports a
// TLS handshake timeout even though TCP came up. The rest pass through.
type flakyTransport struct {
	failures int
	inner    http.RoundTripper
	calls    int
}

func (f *flakyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls++
	if f.failures > 0 {
		f.failures--
		return nil, errors.New("net/http: TLS handshake timeout")
	}
	return f.inner.RoundTrip(r)
}

func flakyPlaid(t *testing.T, failures int, routes map[string]func(*testing.T, map[string]any) (int, string)) (*Plaid, *flakyTransport) {
	t.Helper()
	srv := plaidTestServer(t, routes)
	t.Cleanup(srv.Close)
	tr := &flakyTransport{failures: failures, inner: srv.Client().Transport}
	return NewPlaid("client-id", "secret", "goxero", false,
		WithPlaidBaseURL(srv.URL), WithPlaidHTTPClient(&http.Client{Transport: tr})), tr
}

// TestPlaidRetriesOnlyWhatMayBeRepeated pins the retry policy, and the policy is
// the whole point: a call that fails on the wire is cheap to repeat only when
// repeating it cannot change anything.
//
// The stall it exists for is not Plaid's. A content filter or proxy on the host
// can hold a flow's packets while it decides — macOS diverts such a flow to a
// userspace provider — and the request then dies with a TLS handshake timeout
// while TCP is healthy. The identical bytes succeed a moment later, so a sync
// that would have failed fails no more.
func TestPlaidRetriesOnlyWhatMayBeRepeated(t *testing.T) {
	t.Parallel()
	cred := Credential{Reference: "item-42", Secret: "access-sandbox-7"}

	// A cursor sync that stalled on the wire is sent again and the caller never
	// learns it was ever in doubt.
	p, tr := flakyPlaid(t, 1, map[string]func(*testing.T, map[string]any) (int, string){
		"/transactions/sync": func(*testing.T, map[string]any) (int, string) {
			return 200, `{
				"added":[{"transaction_id":"tx-1","account_id":"acc-chk","amount":-4.50,
				 "iso_currency_code":"USD","date":"2026-04-01","name":"UBER"}],
				"modified":[],"removed":[],"next_cursor":"cursor-1","has_more":false
			}`
		},
	})
	lines, _, cursor, err := p.SyncConnection(context.Background(), cred, "")
	require.NoError(t, err, "a stall on the wire must not fail a sync")
	require.Len(t, lines, 1)
	assert.Equal(t, "cursor-1", cursor)
	assert.Equal(t, 2, tr.calls, "the stalled attempt is repeated exactly once")

	// The exchange is what creates the Item, so it is never repeated: a second
	// attempt mints a second Item, and one whose first attempt had actually
	// succeeded would come back an error and cost the user the consent they just
	// gave. A failed exchange is a failure the user has to see.
	p, tr = flakyPlaid(t, 1, map[string]func(*testing.T, map[string]any) (int, string){
		"/item/public_token/exchange": func(*testing.T, map[string]any) (int, string) {
			return 200, `{"access_token":"access-sandbox-7","item_id":"item-42"}`
		},
	})
	_, err = p.ExchangePublicToken(context.Background(), "public-sandbox-x")
	require.Error(t, err)
	assert.Equal(t, 1, tr.calls, "creating the Item is not a repeatable call")

	// Plaid answering is not a failure to retry. An Item that needs its login
	// redone answers the same way however many times it is asked.
	p, tr = flakyPlaid(t, 0, map[string]func(*testing.T, map[string]any) (int, string){
		"/transactions/sync": func(*testing.T, map[string]any) (int, string) {
			return 400, `{"error_code":"ITEM_LOGIN_REQUIRED",
				"error_message":"the login details of this item have changed"}`
		},
	})
	_, _, _, err = p.SyncConnection(context.Background(), cred, "")
	var apiErr *PlaidError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "ITEM_LOGIN_REQUIRED", apiErr.Code)
	assert.Equal(t, 1, tr.calls, "an API refusal is an answer, not a stall")

	// And a caller who has given up is not kept waiting by our own retry.
	p, tr = flakyPlaid(t, 5, map[string]func(*testing.T, map[string]any) (int, string){
		"/transactions/sync": func(*testing.T, map[string]any) (int, string) {
			return 200, `{"added":[],"modified":[],"removed":[],"next_cursor":"c","has_more":false}`
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err = p.SyncConnection(ctx, cred, "")
	require.Error(t, err)
	assert.Equal(t, 1, tr.calls, "an abandoned request is not retried")
}
