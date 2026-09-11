package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cover the four row panels of the reconcile screen — Match,
// Create, Transfer and Discuss — against the shapes the Xero reference page
// measures. The panel is a client-side thing, but every action inside it ends
// at one of these endpoints, so what is asserted here is the part that can be
// wrong in a way the UI cannot hide: what a filter really selects, what a
// selection really commits, and what an adjustment really posts.

// matchCandidates calls step 1's search with the filters the form sends.
func matchCandidates(t *testing.T, h *appHarness, lineID, query string) []struct {
	BankTransactionID string `json:"BankTransactionID"`
	Type              string `json:"Type"`
	Total             string `json:"Total"`
	CurrencyCode      string `json:"CurrencyCode"`
} {
	t.Helper()
	status, body := h.do(t, http.MethodGet, "/api/v1/statement-lines/"+lineID+"/matches"+query, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var out struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
			Type              string `json:"Type"`
			Total             string `json:"Total"`
			CurrencyCode      string `json:"CurrencyCode"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.BankTransactions
}

// candidatesOfType is the id-by-type view the assertions read more easily.
func candidatesOfType(rows []struct {
	BankTransactionID string `json:"BankTransactionID"`
	Type              string `json:"Type"`
	Total             string `json:"Total"`
	CurrencyCode      string `json:"CurrencyCode"`
}) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		out[r.BankTransactionID] = r.Type
	}
	return out
}

// TestHTTP_BankStatementMatchCandidatesFilters covers the four controls above
// the results table. Each one narrows the same list on the server rather than
// hiding rows in the browser, so "Showing X - Y of Z" counts what the filter
// really matched — which means the filters have to be asserted here, not in the
// component.
func TestHTTP_BankStatementMatchCandidatesFilters(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	var spendLine string
	for _, l := range lines {
		if l.Amount == "-42.5" {
			spendLine = l.StatementLineID
		}
	}
	require.NotEmpty(t, spendLine, "the OFX fixture has a 42.50 debit")

	// Candidates on the same side of the line, one on the other, one in
	// another currency, and one of a size nothing else is.
	sameSide := createBankTransaction(t, h, bank.AccountID.String(), "SPEND", "2026-01-05T00:00:00Z", 42.5, code)
	otherSide := createBankTransaction(t, h, bank.AccountID.String(), "RECEIVE", "2026-01-05T00:00:00Z", 42.5, code)
	oddSize := createBankTransaction(t, h, bank.AccountID.String(), "SPEND", "2026-01-06T00:00:00Z", 99, code)

	status, body := h.do(t, http.MethodPost, "/api/v1/bank-transactions", map[string]any{
		"Type":          "SPEND",
		"BankAccountID": bank.AccountID.String(),
		"Date":          "2026-01-06T00:00:00Z",
		"CurrencyCode":  "NZD",
		"LineItems": []map[string]any{{
			"Description": "another currency", "Quantity": 1, "UnitAmount": 42.5, "AccountCode": code,
		}},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.BankTransactions, 1)
	otherCurrency := created.BankTransactions[0].BankTransactionID

	// "Show Spent Items" off — Xero's default — offers only what moved the
	// way the line did. A receive cannot be the same money as a spend.
	def := candidatesOfType(matchCandidates(t, h, spendLine, ""))
	assert.Contains(t, def, sameSide)
	assert.NotContains(t, def, otherSide, "the opposite direction is hidden until it is asked for")

	// On, and the other side appears.
	shown := candidatesOfType(matchCandidates(t, h, spendLine, "?showSpent=true"))
	assert.Contains(t, shown, sameSide)
	assert.Contains(t, shown, otherSide)

	// "Show NZD items only".
	nzd := candidatesOfType(matchCandidates(t, h, spendLine, "?currency=NZD"))
	assert.Equal(t, map[string]string{otherCurrency: "SPEND"}, nzd)

	// "Search by amount" is on the transaction's total, not its text.
	byAmount := candidatesOfType(matchCandidates(t, h, spendLine, "?amount=99"))
	assert.Equal(t, map[string]string{oddSize: "SPEND"}, byAmount)

	// A filter with no matches is an empty list, not an error.
	assert.Empty(t, matchCandidates(t, h, spendLine, "?amount=1234.56"))

	// A non-numeric amount is refused rather than silently ignored.
	status, body = h.do(t, http.MethodGet, "/api/v1/statement-lines/"+spendLine+"/matches?amount=abc", nil, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))

	// A transaction the user has deleted stops being a candidate. Deleting only
	// sets status='DELETED', so it has to be excluded by status or the table
	// keeps offering money that is gone and counts it in "Showing X - Y of Z".
	require.NotEmpty(t, candidatesOfType(matchCandidates(t, h, spendLine, "?amount=99")))
	status, body = h.do(t, http.MethodDelete, "/api/v1/bank-transactions/"+oddSize, nil, true)
	require.Equal(t, http.StatusNoContent, status, string(body))
	assert.Empty(t, matchCandidates(t, h, spendLine, "?amount=99"),
		"a deleted transaction is not offered as a match")
}

// TestHTTP_BankStatementReconcileSelection covers step 3's commit: several
// transactions whose signed sum is the statement line, reconciled together.
func TestHTTP_BankStatementReconcileSelection(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	var spendLine string
	for _, l := range lines {
		if l.Amount == "-42.5" {
			spendLine = l.StatementLineID
		}
	}
	require.NotEmpty(t, spendLine)

	// Two spends that add up to the line, and one that does not.
	part := createBankTransaction(t, h, bank.AccountID.String(), "SPEND", "2026-01-05T00:00:00Z", 30, code)
	rest := createBankTransaction(t, h, bank.AccountID.String(), "SPEND", "2026-01-06T00:00:00Z", 12.5, code)
	wrong := createBankTransaction(t, h, bank.AccountID.String(), "SPEND", "2026-01-06T00:00:00Z", 5, code)

	// A selection that does not add up is refused, and nothing is reconciled.
	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/reconcile",
		map[string]any{"BankTransactionIDs": []string{part, wrong}}, true)
	require.Equal(t, http.StatusBadRequest, status, string(body))
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 2,
		"a refused selection leaves the line in the inbox")

	// The same transaction twice is refused rather than counted twice.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/reconcile",
		map[string]any{"BankTransactionIDs": []string{part, part}}, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))

	// An empty selection has nothing to commit.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/reconcile",
		map[string]any{"BankTransactionIDs": []string{}}, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))

	// Both together are the line.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/reconcile",
		map[string]any{"BankTransactionIDs": []string{part, rest}}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 1,
		"the reconciled line leaves the inbox")

	status, body = h.do(t, http.MethodGet, "/api/v1/bank-transactions?bankAccountId="+bank.AccountID.String(), nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var all struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
			IsReconciled      bool   `json:"IsReconciled"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &all))
	reconciled := map[string]bool{}
	for _, bt := range all.BankTransactions {
		reconciled[bt.BankTransactionID] = bt.IsReconciled
	}
	assert.True(t, reconciled[part], "every transaction in the selection is reconciled")
	assert.True(t, reconciled[rest])
	assert.False(t, reconciled[wrong], "one that was not selected is left alone")
}

// TestHTTP_BankStatementCreateWithoutReconcile covers step 2's "New
// Transaction": the transaction is posted so it can be selected, which means it
// must not reconcile the line or itself on the way in.
func TestHTTP_BankStatementCreateWithoutReconcile(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	var spendLine string
	for _, l := range lines {
		if l.Amount == "-42.5" {
			spendLine = l.StatementLineID
		}
	}
	require.NotEmpty(t, spendLine)

	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/create",
		map[string]any{"AccountCode": code, "Reconcile": false}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
			IsReconciled      bool   `json:"IsReconciled"`
			Type              string `json:"Type"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.BankTransactions, 1)
	assert.False(t, created.BankTransactions[0].IsReconciled,
		"a transaction added to the selection is not the answer yet")
	assert.Equal(t, "SPEND", created.BankTransactions[0].Type, "the line was a debit")

	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 2,
		"the line is still waiting")

	// The default is still the old behaviour: created and reconciled in one.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/create",
		map[string]any{"AccountCode": code, "BankTransactionID": created.BankTransactions[0].BankTransactionID}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 1)
}

// TestHTTP_BankStatementAdjustment covers step 3's "Adjustments" menu. A bank
// fee and a minor adjustment are the same operation under two descriptions:
// post the shortfall to an account, unreconciled, so it can join the selection
// like any other candidate.
//
// Which account is the question. Xero gives no account a bank-fee role, so the
// answer has to come out of the organisation's own books, and the test arranges
// the books rather than naming a code -- it books one bank fee first and then
// asserts the next one follows it.
func TestHTTP_BankStatementAdjustment(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	var spendLine string
	for _, l := range lines {
		if l.Amount == "-42.5" {
			spendLine = l.StatementLineID
		}
	}
	require.NotEmpty(t, spendLine)

	// Part of the line, then the fee that closes the gap.
	part := createBankTransaction(t, h, bank.AccountID.String(), "SPEND", "2026-01-05T00:00:00Z", 30, code)

	// The wrong kind is refused rather than posted.
	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/adjustment",
		map[string]any{"Kind": "SOMETHING_ELSE", "Amount": "-12.50"}, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))

	// So is one that moves the other way: an adjustment is part of the line.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/adjustment",
		map[string]any{"Kind": "BANK_FEE", "Amount": "12.50"}, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))

	// And one for nothing at all.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/adjustment",
		map[string]any{"Kind": "BANK_FEE", "Amount": "0"}, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))

	// With no account named and nothing in these books to follow, the request is
	// refused rather than given an account chosen by its position in the chart:
	// the selection list shows no account before the line is committed, so a
	// posting nobody chose is a posting nobody sees.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/adjustment",
		map[string]any{"Kind": "MINOR_ADJUSTMENT", "Amount": "-12.50"}, true)
	require.Equal(t, http.StatusBadRequest, status, string(body))

	// These books do know where a bank fee goes: the last one was booked to an
	// account of the organisation's choosing, and the next one follows it. The
	// account is read from the chart rather than named, and it is deliberately
	// not the first expense account -- so a default taken from the chart's order
	// would answer with a different code and fail here.
	feeAccount := nthExpenseCode(t, h, 1)
	require.NotEqual(t, nthExpenseCode(t, h, 0), feeAccount)
	createDescribedBankTransaction(t, h, bank.AccountID.String(), "SPEND", "2026-01-04T00:00:00Z", 7.25, feeAccount, "Bank fee")

	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/adjustment",
		map[string]any{"Kind": "BANK_FEE", "Amount": "-12.50"}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
			IsReconciled      bool   `json:"IsReconciled"`
			Type              string `json:"Type"`
			Total             string `json:"Total"`
			LineItems         []struct {
				Description string `json:"Description"`
				AccountCode string `json:"AccountCode"`
			} `json:"LineItems"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.BankTransactions, 1)
	fee := created.BankTransactions[0]
	assert.False(t, fee.IsReconciled, "the adjustment joins the selection rather than answering it")
	assert.Equal(t, "SPEND", fee.Type)
	assert.Equal(t, "12.5", fee.Total)
	require.Len(t, fee.LineItems, 1)
	assert.Equal(t, "Bank fee", fee.LineItems[0].Description)
	assert.Equal(t, feeAccount, fee.LineItems[0].AccountCode,
		"an adjustment with no account named follows the account these books last booked a bank fee to")

	// The line is untouched until the selection is committed, and then the two
	// together are the line.
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 2)
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/reconcile",
		map[string]any{"BankTransactionIDs": []string{part, fee.BankTransactionID}}, true)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 1)

	// The other menu item differs only in what it writes.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/adjustment",
		map[string]any{"Kind": "MINOR_ADJUSTMENT", "Amount": "-1.00", "AccountCode": code}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var second struct {
		BankTransactions []struct {
			LineItems []struct {
				Description string `json:"Description"`
				AccountCode string `json:"AccountCode"`
			} `json:"LineItems"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &second))
	require.Len(t, second.BankTransactions, 1)
	require.Len(t, second.BankTransactions[0].LineItems, 1)
	assert.Equal(t, "Minor adjustment", second.BankTransactions[0].LineItems[0].Description)
	assert.Equal(t, code, second.BankTransactions[0].LineItems[0].AccountCode,
		"a named account is used as given")
}
