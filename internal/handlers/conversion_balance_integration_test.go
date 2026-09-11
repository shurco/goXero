package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type conversionBalanceBody struct {
	ConversionDate string `json:"ConversionDate"`
	Locked         bool   `json:"Locked"`
	Lines          []struct {
		AccountID string          `json:"AccountID"`
		Code      string          `json:"Code"`
		Amount    decimal.Decimal `json:"Amount"`
	} `json:"Lines"`
}

func (h *appHarness) conversionBalance(t *testing.T) conversionBalanceBody {
	t.Helper()
	status, body := h.do(t, http.MethodGet, "/api/v1/conversion-balances", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var env struct {
		ConversionBalance conversionBalanceBody `json:"ConversionBalance"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	return env.ConversionBalance
}

// accountID is the demo organisation's account with this code, as the API's own
// rows address accounts (by UUID, not by code).
func (h *appHarness) accountID(t *testing.T, code string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, h.repos.Pool.QueryRow(context.Background(),
		`SELECT account_id FROM accounts WHERE organisation_id=$1 AND code=$2`,
		seedDemoOrgID, code).Scan(&id))
	return id
}

// conversionJournalLines is what the organisation's conversion journal holds on
// one account -- read from the ledger, not from the screen, so the two can be
// compared.
func (h *appHarness) conversionJournalLines(t *testing.T, accountID uuid.UUID) decimal.Decimal {
	t.Helper()
	var net decimal.Decimal
	require.NoError(t, h.repos.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(l.net_amount),0)
		   FROM gl_journal_lines l
		   JOIN gl_journals j USING (journal_id)
		  WHERE j.organisation_id=$1 AND j.source_type='CONVERSIONBALANCE' AND l.account_id=$2`,
		seedDemoOrgID, accountID).Scan(&net))
	return net
}

func (h *appHarness) conversionJournalCount(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, h.repos.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM gl_journals WHERE organisation_id=$1 AND source_type='CONVERSIONBALANCE'`,
		seedDemoOrgID).Scan(&n))
	return n
}

func (h *appHarness) setOrganisationRole(t *testing.T, role string) {
	t.Helper()
	_, err := h.repos.Pool.Exec(context.Background(),
		`UPDATE organisation_users SET role=$3 WHERE user_id=$1 AND organisation_id=$2`,
		seedDemoUserID, seedDemoOrgID, role)
	require.NoError(t, err)
}

// The reference organisation's opening balance is Xero's journal 388
// "Conversion Balance" of 21 Jun 2026. The screen is where it has to appear.
// Before this endpoint existed the screen showed an empty form over a ledger
// that already held 4,130.98 of opening balances -- and Save wrote nothing at
// all, so entering them again would have double-counted them.
func TestHTTP_ConversionBalances_ShowTheReferenceOpeningBalance(t *testing.T) {
	h := newHarness(t)

	cb := h.conversionBalance(t)
	assert.Equal(t, "2026-06-21", cb.ConversionDate)
	assert.False(t, cb.Locked)
	require.Len(t, cb.Lines, 2)
	assert.Equal(t, "090", cb.Lines[0].Code)
	assert.True(t, decimal.RequireFromString("4130.98").Equal(cb.Lines[0].Amount),
		"090 carries journal 388's debit, got %s", cb.Lines[0].Amount)
	assert.Equal(t, "840", cb.Lines[1].Code)
	assert.True(t, decimal.RequireFromString("-4130.98").Equal(cb.Lines[1].Amount),
		"840 carries journal 388's credit, got %s", cb.Lines[1].Amount)

	// One journal, adopted from the ledger -- not a second set of balances
	// standing beside the one 00024 already posted.
	assert.Equal(t, 1, h.conversionJournalCount(t))
}

// Saving stores the balances as the organisation's conversion journal, in the
// ledger where the Trial Balance and the Balance Sheet read them. A set whose
// debits and credits differ is not refused and not silently dropped: the
// difference is posted to the Historical Adjustment account, which is what the
// screen's "Adjustments" figure is.
func TestHTTP_ConversionBalances_SavePostsToTheLedger(t *testing.T) {
	h := newHarness(t)
	bank, inventory := h.accountID(t, "090"), h.accountID(t, "630")

	status, body := h.do(t, http.MethodPut, "/api/v1/conversion-balances", map[string]any{
		"ConversionDate": "2026-01-01",
		"Locked":         false,
		"Lines": []map[string]any{
			{"AccountID": bank, "Amount": "100"},
			{"AccountID": inventory, "Amount": "-40"},
		},
	}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	var env struct {
		ConversionBalance conversionBalanceBody `json:"ConversionBalance"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	assert.Equal(t, "2026-01-01", env.ConversionBalance.ConversionDate)
	require.Len(t, env.ConversionBalance.Lines, 3, "the two entered lines plus the 60.00 adjustment")
	assert.Equal(t, "840", env.ConversionBalance.Lines[2].Code)
	assert.True(t, decimal.NewFromInt(-60).Equal(env.ConversionBalance.Lines[2].Amount))

	assert.True(t, decimal.NewFromInt(100).Equal(h.conversionJournalLines(t, bank)),
		"090 must carry 100, not 100 plus journal 388's 4,130.98")
	assert.True(t, decimal.NewFromInt(-60).Equal(h.conversionJournalLines(t, h.accountID(t, "840"))),
		"840 carries the adjustment alone: journal 388's -4,130.98 went with it")

	// Saving again replaces the journal rather than adding a second one.
	status, body = h.do(t, http.MethodPut, "/api/v1/conversion-balances", map[string]any{
		"ConversionDate": "2026-01-01",
		"Locked":         false,
		"Lines": []map[string]any{
			{"AccountID": bank, "Amount": "100"},
			{"AccountID": inventory, "Amount": "-40"},
		},
	}, true)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Equal(t, 1, h.conversionJournalCount(t))
	assert.True(t, decimal.NewFromInt(100).Equal(h.conversionJournalLines(t, bank)))

	// Every journal in the ledger still balances: the save changed what the
	// opening balances are, not whether the books add up.
	var unbalanced int
	require.NoError(t, h.repos.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM (
		     SELECT l.journal_id FROM gl_journal_lines l
		     JOIN gl_journals j USING (journal_id)
		     WHERE j.organisation_id=$1
		     GROUP BY l.journal_id HAVING SUM(l.net_amount) <> 0) x`,
		seedDemoOrgID).Scan(&unbalanced))
	assert.Zero(t, unbalanced)
}

// A row is refused when it cannot become a ledger line: an unknown account, the
// same account twice, or a date that is not a calendar date. Refusing is the
// point -- the alternative is a balance posted to nobody in particular.
func TestHTTP_ConversionBalances_RefuseRowsThatCannotPost(t *testing.T) {
	h := newHarness(t)
	bank := h.accountID(t, "090")

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{
			name: "unknown account",
			body: map[string]any{"ConversionDate": "2026-01-01", "Lines": []map[string]any{
				{"AccountID": uuid.NewString(), "Amount": "10"},
			}},
			want: http.StatusNotFound,
		},
		{
			name: "account entered twice",
			body: map[string]any{"ConversionDate": "2026-01-01", "Lines": []map[string]any{
				{"AccountID": bank, "Amount": "10"},
				{"AccountID": bank, "Amount": "-10"},
			}},
			want: http.StatusBadRequest,
		},
		{
			name: "date is not a calendar date",
			body: map[string]any{"ConversionDate": "01/01/2026", "Lines": []map[string]any{
				{"AccountID": bank, "Amount": "10"},
			}},
			want: http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := h.do(t, http.MethodPut, "/api/v1/conversion-balances", tc.body, true)
			assert.Equal(t, tc.want, status, string(body))
		})
	}
}

// Locking is what the lock is for: it stops the balances changing by accident.
// Xero lets an adviser past it and no one else, and the screen says so ("Only
// users with Adviser roles will be able to make any changes"), so a member with
// any other role can neither edit the balances nor lift the lock.
func TestHTTP_ConversionBalances_LockedNeedsAnAdministrator(t *testing.T) {
	h := newHarness(t)
	bank := h.accountID(t, "090")

	locked := map[string]any{
		"ConversionDate": "2026-01-01",
		"Locked":         true,
		"Lines":          []map[string]any{{"AccountID": bank, "Amount": "10"}},
	}
	status, body := h.do(t, http.MethodPut, "/api/v1/conversion-balances", locked, true)
	require.Equal(t, http.StatusOK, status, string(body))

	h.setOrganisationRole(t, "STANDARD")
	status, body = h.do(t, http.MethodPut, "/api/v1/conversion-balances", map[string]any{
		"ConversionDate": "2026-02-01",
		"Locked":         false,
		"Lines":          []map[string]any{{"AccountID": bank, "Amount": "999"}},
	}, true)
	assert.Equal(t, http.StatusForbidden, status, string(body))

	cb := h.conversionBalance(t)
	assert.True(t, cb.Locked, "the refused save must not have lifted the lock")
	assert.Equal(t, "2026-01-01", cb.ConversionDate)
	assert.True(t, decimal.NewFromInt(10).Equal(h.conversionJournalLines(t, bank)),
		"the refused save must not have changed the balances")

	h.setOrganisationRole(t, "ADMIN")
	status, body = h.do(t, http.MethodPut, "/api/v1/conversion-balances", map[string]any{
		"ConversionDate": "2026-02-01",
		"Locked":         false,
		"Lines":          []map[string]any{{"AccountID": bank, "Amount": "10"}},
	}, true)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.False(t, h.conversionBalance(t).Locked)
	assert.Equal(t, "2026-02-01", h.conversionBalance(t).ConversionDate)
}
