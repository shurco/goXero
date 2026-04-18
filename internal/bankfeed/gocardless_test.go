package bankfeed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMapGoCardlessTx ensures we preserve amount sign, pick a stable provider
// id, and prefer booking date over value date. These invariants are load-
// bearing — the statement_lines UNIQUE constraint relies on ProviderTxID.
func TestMapGoCardlessTx(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		payload   string
		wantID    string
		wantAmt   string
		wantDesc  string
		wantParty string
	}{
		{
			name: "credit with transactionId",
			payload: `{
				"transactionId":"tx-1",
				"bookingDate":"2026-04-01",
				"transactionAmount":{"amount":"120.50","currency":"EUR"},
				"creditorName":"Acme",
				"remittanceInformationUnstructured":"Invoice 42"
			}`,
			wantID: "tx-1", wantAmt: "120.5", wantDesc: "Invoice 42", wantParty: "Acme",
		},
		{
			name: "debit fallback id",
			payload: `{
				"internalTransactionId":"int-9",
				"valueDate":"2026-03-29",
				"transactionAmount":{"amount":"-45.00","currency":"EUR"},
				"debtorName":"Utility Co"
			}`,
			wantID: "int-9", wantAmt: "-45", wantDesc: "", wantParty: "Utility Co",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			line, err := MapGoCardlessTx([]byte(tc.payload))
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, line.ProviderTxID)
			assert.True(t, line.Amount.Equal(decimal.RequireFromString(tc.wantAmt)))
			assert.Equal(t, tc.wantDesc, line.Description)
			assert.Equal(t, tc.wantParty, line.Counterparty)
		})
	}
}

// TestMapGoCardlessTx_Rejects guards against lossy rows — an entry without
// any usable id would break dedup.
func TestMapGoCardlessTx_Rejects(t *testing.T) {
	t.Parallel()
	_, err := MapGoCardlessTx([]byte(`{"transactionAmount":{"amount":"1","currency":"EUR"}}`))
	require.Error(t, err)
}

// TestGoCardless_ListInstitutions drives the adapter against an httptest
// server to cover the auth + request-signing plumbing.
func TestGoCardless_ListInstitutions(t *testing.T) {
	t.Parallel()
	var tokenHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token/new/"):
			tokenHits++
			raw, _ := io.ReadAll(r.Body)
			require.Contains(t, string(raw), `"secret_id":"id"`)
			_ = json.NewEncoder(w).Encode(map[string]any{"access": "T", "access_expires": 3600})
		case strings.HasSuffix(r.URL.Path, "/institutions/"):
			assert.Equal(t, "Bearer T", r.Header.Get("Authorization"))
			assert.Equal(t, "GB", r.URL.Query().Get("country"))
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": "BANK_X", "name": "Bank X", "bic": "BANKXGB00",
				"countries": []string{"GB"}, "transaction_total_days": "730",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gc := NewGoCardless("id", "key",
		WithGoCardlessBaseURL(srv.URL),
		WithGoCardlessHTTPClient(srv.Client()),
	)
	require.True(t, gc.Credentials())

	inst, err := gc.ListInstitutions(context.Background(), "gb")
	require.NoError(t, err)
	require.Len(t, inst, 1)
	assert.Equal(t, "BANK_X", inst[0].ID)
	assert.Equal(t, 730, inst[0].TxDays)

	// Second call should reuse the cached access token.
	_, err = gc.ListInstitutions(context.Background(), "gb")
	require.NoError(t, err)
	assert.Equal(t, 1, tokenHits, "token must be cached between calls")
}
