package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/repository"
)

// The balance graph on a bank account card is one line per account over the
// last month, and the figure it ends on is the same "Balance in Xero" the card
// prints above it. Both halves of that are server-side claims — the window, and
// the fact that a day nothing posted still gets a point — so they are asserted
// here rather than in the component, which can only draw what it is given.

type balanceSeriesPoint struct {
	Date    string `json:"Date"`
	Balance string `json:"Balance"`
}

func balanceSeries(t *testing.T, h *appHarness, bankAccountID, query string) []balanceSeriesPoint {
	t.Helper()
	status, body := h.do(t, http.MethodGet,
		"/api/v1/statement-lines/balance-series?bankAccountId="+bankAccountID+query, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var out struct {
		BalanceSeries []balanceSeriesPoint `json:"BalanceSeries"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.BalanceSeries
}

func ledgerBalance(t *testing.T, h *appHarness, bankAccountID string) string {
	t.Helper()
	status, body := h.do(t, http.MethodGet,
		"/api/v1/statement-lines/balance?bankAccountId="+bankAccountID, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var out struct {
		Balance struct {
			LedgerBalance string `json:"LedgerBalance"`
		} `json:"Balance"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.Balance.LedgerBalance
}

// TestHTTP_BalanceSeriesIsAMonthOfDays checks the shape of the window: one
// point per calendar day, ending today, with no day missing.
func TestHTTP_BalanceSeriesIsAMonthOfDays(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)

	series := balanceSeries(t, h, bank.AccountID.String(), "")
	require.Len(t, series, repository.BalanceSeriesDays)

	for i := 1; i < len(series); i++ {
		prev, err := time.Parse("2006-01-02", series[i-1].Date)
		require.NoError(t, err)
		next, err := time.Parse("2006-01-02", series[i].Date)
		require.NoError(t, err)
		assert.Equal(t, 24*time.Hour, next.Sub(prev), "day %d follows its predecessor", i)
	}

	// An account nothing has ever been posted to is empty on every day, not
	// absent on every day: the graph still has a line to draw, at zero.
	for _, p := range series {
		assert.Equal(t, "0", p.Balance, "day %s", p.Date)
	}

	// The width is a parameter, so a caller can ask for a shorter window.
	assert.Len(t, balanceSeries(t, h, bank.AccountID.String(), "&days=7"), 7)

	// A width that is not a number is refused rather than quietly replaced
	// with the default, which would answer a different question.
	status, body := h.do(t, http.MethodGet,
		"/api/v1/statement-lines/balance-series?bankAccountId="+bank.AccountID.String()+"&days=weekly", nil, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))

	// A series without an account to draw is a mistake, not an empty graph.
	status, body = h.do(t, http.MethodGet, "/api/v1/statement-lines/balance-series", nil, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))
}

// TestHTTP_BalanceSeriesCarriesTheBalanceForward is the staircase: a day
// nothing posted repeats the day before rather than dropping out, and the last
// point is the number printed above the graph.
func TestHTTP_BalanceSeriesCarriesTheBalanceForward(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	span := balanceSeries(t, h, bank.AccountID.String(), "")
	require.Len(t, span, repository.BalanceSeriesDays)
	end, err := time.Parse("2006-01-02", span[len(span)-1].Date)
	require.NoError(t, err)
	posted := end.AddDate(0, 0, -10)

	status, body := h.do(t, http.MethodPost, "/api/v1/bank-transactions", map[string]any{
		"Type":          "RECEIVE",
		"BankAccountID": bank.AccountID.String(),
		"Date":          posted.Format("2006-01-02T15:04:05Z"),
		"LineItems": []map[string]any{{
			"Description": "carried forward", "Quantity": 1, "UnitAmount": 100, "AccountCode": code,
		}},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	series := balanceSeries(t, h, bank.AccountID.String(), "")
	require.Len(t, series, repository.BalanceSeriesDays)

	for _, p := range series {
		day, err := time.Parse("2006-01-02", p.Date)
		require.NoError(t, err)
		if day.Before(posted) {
			assert.Equal(t, "0", p.Balance, "the account was empty on %s", p.Date)
			continue
		}
		assert.Equal(t, "100", p.Balance, "%s repeats the day before", p.Date)
	}

	// The line ends on the figure the card prints above it.
	assert.Equal(t, ledgerBalance(t, h, bank.AccountID.String()), series[len(series)-1].Balance)
}

// TestHTTP_BalanceSeriesEndsOnTheLedgerBalance pins the invariant on the seeded
// tenant rather than on a fixture this test built: the graph's last point and
// the card's "Balance in Xero" come from two different queries, and a month of
// real postings is what proves they agree.
func TestHTTP_BalanceSeriesEndsOnTheLedgerBalance(t *testing.T) {
	h := newHarness(t)

	accounts, err := h.repos.Accounts.List(t.Context(), seedDemoOrgID, repository.AccountFilter{})
	require.NoError(t, err)
	var bankAccountID string
	for _, a := range accounts {
		if a.Code == "090" {
			bankAccountID = a.AccountID.String()
		}
	}
	require.NotEmpty(t, bankAccountID, "the seeded chart has no 090 bank account")

	series := balanceSeries(t, h, bankAccountID, "")
	require.Len(t, series, repository.BalanceSeriesDays)
	assert.Equal(t, ledgerBalance(t, h, bankAccountID), series[len(series)-1].Balance,
		"the graph must end on the balance the card prints above it")
}
