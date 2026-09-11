package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// doUpload posts a multipart form, which is how the statement import wizard
// sends the file. h.do() only speaks JSON.
func (h *appHarness) doUpload(t *testing.T, path, filename string, content []byte, fields map[string]string) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	fw, err := mw.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Xero-Tenant-Id", seedDemoOrgID.String())
	resp, err := h.app.Test(req, fiberTestConfig)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode, readAll(t, resp.Body)
}

// A small OFX 1.x file — the SGML dialect that never closes its leaf elements.
const testOFX = `OFXHEADER:100
DATA:OFXSGML
VERSION:102
SECURITY:NONE
ENCODING:USASCII

<OFX>
<BANKMSGSRSV1>
<STMTTRNRS>
<STMTRS>
<CURDEF>USD
<BANKACCTFROM>
<BANKID>121000248
<ACCTID>132435465
</BANKACCTFROM>
<BANKTRANLIST>
<DTSTART>20260101
<DTEND>20260131
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20260105
<TRNAMT>-42.50
<FITID>OFX-1
<NAME>STARBUCKS STORE 1234
<MEMO>Coffee
</STMTTRN>
<STMTTRN>
<TRNTYPE>CREDIT
<DTPOSTED>20260107
<TRNAMT>1200.00
<FITID>OFX-2
<NAME>ACME CLIENT PAYMENT
<MEMO>Invoice 1042
</STMTTRN>
</BANKTRANLIST>
<LEDGERBAL>
<BALAMT>1157.50
<DTASOF>20260131
</LEDGERBAL>
</STMTRS>
</STMTTRNRS>
</BANKMSGSRSV1>
</OFX>`

// newStatementAccount hands a test a bank account with nothing in it: no statement
// lines and no transactions. Every test below asserts on an inbox, so the inbox
// has to hold only what the test itself put there — taking the seeded business
// account instead makes the assertions depend on whatever the fixture happens to
// contain, which is how these tests broke the last time the fixture grew. A
// fresh account per call also means two tests can run against the same database
// without meeting each other's lines.
func newStatementAccount(t *testing.T, h *appHarness) models.Account {
	t.Helper()
	acc := models.Account{
		Code:         "T" + uuid.NewString()[:7],
		Name:         "Reconcile test account",
		Type:         "BANK",
		CurrencyCode: "USD",
		Status:       "ACTIVE",
	}
	require.NoError(t, h.repos.Accounts.Create(context.Background(), seedDemoOrgID, &acc))
	require.NotEqual(t, uuid.Nil, acc.AccountID, "the account must have been inserted")
	return acc
}

// firstAccountCode returns a code from the tenant's chart other than the bank
// account, so cash coding has somewhere to post.
func firstAccountCode(t *testing.T, h *appHarness, exclude string) string {
	t.Helper()
	list, err := h.repos.Accounts.List(context.Background(), seedDemoOrgID, repository.AccountFilter{})
	require.NoError(t, err)
	for _, a := range list {
		if a.Code != exclude {
			return a.Code
		}
	}
	t.Fatal("no non-bank account in the fixture")
	return ""
}

// nthExpenseCode returns the code of the nth expense account in the chart's own
// order, counting from zero. A test that cares which account a rule picked reads
// it from the chart rather than naming one, because that is what the rule itself
// has to do: a code is the organisation's own label and the rule is not allowed
// to know it.
func nthExpenseCode(t *testing.T, h *appHarness, n int) string {
	t.Helper()
	list, err := h.repos.Accounts.List(context.Background(), seedDemoOrgID, repository.AccountFilter{Status: "ACTIVE"})
	require.NoError(t, err)
	seen := 0
	for _, a := range list {
		if a.Type != models.AccountTypeExpense {
			continue
		}
		if seen == n {
			return a.Code
		}
		seen++
	}
	t.Fatalf("the fixture chart has fewer than %d expense accounts", n+1)
	return ""
}

// createBankTransaction posts one hand-entered transaction on a bank account
// and returns its id, which is what "Find & match" hunts for.
func createBankTransaction(t *testing.T, h *appHarness, bankAccountID, txType, date string, amount float64, code string) string {
	t.Helper()
	return createDescribedBankTransaction(t, h, bankAccountID, txType, date, amount, code, "hand entered")
}

// createDescribedBankTransaction is createBankTransaction with a description of
// the caller's choosing. The description is what the coding history remembers an
// entry by when it has no counterparty to remember it by.
func createDescribedBankTransaction(t *testing.T, h *appHarness, bankAccountID, txType, date string, amount float64, code, description string) string {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "/api/v1/bank-transactions", map[string]any{
		"Type":          txType,
		"BankAccountID": bankAccountID,
		"Date":          date,
		"LineItems": []map[string]any{{
			"Description": description, "Quantity": 1, "UnitAmount": amount, "AccountCode": code,
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
	return created.BankTransactions[0].BankTransactionID
}

type importResponse struct {
	StatementImports []struct {
		Import struct {
			ImportID       string `json:"ImportID"`
			Status         string `json:"Status"`
			Format         string `json:"Format"`
			LineCount      int    `json:"LineCount"`
			DuplicateCount int    `json:"DuplicateCount"`
			Filename       string `json:"Filename"`
			OpeningBalance string `json:"OpeningBalance"`
			ClosingBalance string `json:"ClosingBalance"`
		} `json:"Import"`
		Preview []struct {
			Date     string `json:"Date"`
			Amount   string `json:"Amount"`
			Payee    string `json:"Payee"`
			Adjacent bool   `json:"Duplicate"`
		} `json:"Preview"`
	} `json:"StatementImports"`
}

type statementLine struct {
	StatementLineID   string `json:"StatementLineID"`
	PostedAt          string `json:"PostedAt"`
	Source            string `json:"Source"`
	Status            string `json:"Status"`
	Amount            string `json:"Amount"`
	Payee             string `json:"Payee"`
	Balance           string `json:"Balance"`
	BankTransactionID string `json:"BankTransactionID"`
	CodedAccountCode  string `json:"CodedAccountCode"`
	CodedAccountName  string `json:"CodedAccountName"`
	AutoReconciledAt  string `json:"AutoReconciledAt"`
}

func listLines(t *testing.T, h *appHarness, query string) []statementLine {
	t.Helper()
	status, body := h.do(t, http.MethodGet, "/api/v1/statement-lines"+query, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var out struct {
		StatementLines []statementLine `json:"StatementLines"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.StatementLines
}

// stageAndCommit walks the wizard's two steps for tests that only care about
// the lines landing in the inbox, and returns the import id.
func stageAndCommit(t *testing.T, h *appHarness, bankAccountID, filename string, content []byte) string {
	t.Helper()
	status, body := h.doUpload(t, "/api/v1/statement-imports", filename, content,
		map[string]string{"bankAccountId": bankAccountID})
	require.Equal(t, http.StatusCreated, status, string(body))
	var staged importResponse
	require.NoError(t, json.Unmarshal(body, &staged))
	require.Len(t, staged.StatementImports, 1)
	id := staged.StatementImports[0].Import.ImportID
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-imports/"+id+"/commit",
		map[string]any{}, true)
	require.Equal(t, http.StatusOK, status, string(body))
	return id
}

// TestHTTP_BankStatementImportEndToEnd walks the whole manual-import flow the
// wizard drives: upload and stage, review the preview, commit, reconcile a line
// into a bank transaction, then re-import the same file and have the duplicates
// held back.
func TestHTTP_BankStatementImportEndToEnd(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	// --- Step 1: upload. The file is parsed, staged, and previewed. ---
	status, body := h.doUpload(t, "/api/v1/statement-imports", "january.ofx",
		[]byte(testOFX), map[string]string{"bankAccountId": bank.AccountID.String()})
	require.Equal(t, http.StatusCreated, status, string(body))
	var staged importResponse
	require.NoError(t, json.Unmarshal(body, &staged))
	require.Len(t, staged.StatementImports, 1)
	imp := staged.StatementImports[0].Import
	assert.Equal(t, models.StatementImportStaged, imp.Status)
	assert.Equal(t, "OFX", imp.Format)
	assert.Equal(t, 2, imp.LineCount)
	assert.Equal(t, 0, imp.DuplicateCount)
	assert.Equal(t, "january.ofx", imp.Filename)
	require.Len(t, staged.StatementImports[0].Preview, 2)
	assert.Equal(t, "2026-01-05", staged.StatementImports[0].Preview[0].Date)
	assert.Equal(t, "-42.5", staged.StatementImports[0].Preview[0].Amount)
	assert.Equal(t, "STARBUCKS STORE 1234", staged.StatementImports[0].Preview[0].Payee)

	// Nothing is in the inbox until the user commits.
	assert.Empty(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()))

	// --- Step 2: commit. ---
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-imports/"+imp.ImportID+"/commit",
		map[string]any{"IncludeDuplicates": false}, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var committed struct {
		Imported int `json:"Imported"`
		Skipped  int `json:"Skipped"`
	}
	require.NoError(t, json.Unmarshal(body, &committed))
	assert.Equal(t, 2, committed.Imported)
	assert.Equal(t, 0, committed.Skipped)

	// --- Step 3: the lines are in the inbox, tagged as imported. ---
	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	require.Len(t, lines, 2)
	for _, l := range lines {
		assert.Equal(t, models.StatementLineSourceImport, l.Source)
		assert.Equal(t, models.BankFeedLineStatusNew, l.Status)
	}

	// --- Step 4: the reconcile header. Nothing is posted yet, so the ledger
	// balance is untouched and the statement balance is the net of the inbox. ---
	status, body = h.do(t, http.MethodGet, "/api/v1/statement-lines/balance?bankAccountId="+bank.AccountID.String(), nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var balance struct {
		Balance struct {
			LedgerBalance     string `json:"LedgerBalance"`
			StatementBalance  string `json:"StatementBalance"`
			UnreconciledCount int    `json:"UnreconciledCount"`
		} `json:"Balance"`
	}
	require.NoError(t, json.Unmarshal(body, &balance))
	assert.Equal(t, "1157.5", balance.Balance.StatementBalance) // 1200.00 - 42.50
	assert.Equal(t, 2, balance.Balance.UnreconciledCount)

	// --- Step 5: cash-code the spend line into a bank transaction. ---
	var spendLine statementLine
	for _, l := range lines {
		if l.Amount == "-42.5" {
			spendLine = l
		}
	}
	require.NotEmpty(t, spendLine.StatementLineID, "the spend line must be in the inbox")
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/cash-code", map[string]any{
		"Rows": []map[string]any{{
			"StatementLineID": spendLine.StatementLineID,
			"AccountCode":     code,
		}},
	}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	// The line is reconciled and now points at the transaction it produced.
	reconciled := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+"&status=IMPORTED")
	require.Len(t, reconciled, 1)
	assert.NotEmpty(t, reconciled[0].BankTransactionID)
	assert.Equal(t, spendLine.StatementLineID, reconciled[0].StatementLineID)

	// The bank transaction is a SPEND for the absolute amount and is reconciled.
	status, body = h.do(t, http.MethodGet, "/api/v1/bank-transactions/"+reconciled[0].BankTransactionID, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var bt struct {
		Payload struct {
			BankTransactions []struct {
				Type         string `json:"Type"`
				Total        string `json:"Total"`
				IsReconciled bool   `json:"IsReconciled"`
			} `json:"BankTransactions"`
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &bt))
	require.Len(t, bt.Payload.BankTransactions, 1)
	assert.Equal(t, models.BankTransactionTypeSpend, bt.Payload.BankTransactions[0].Type)
	assert.Equal(t, "42.5", bt.Payload.BankTransactions[0].Total)
	assert.True(t, bt.Payload.BankTransactions[0].IsReconciled)

	// --- Step 6: re-importing the same file flags both rows as duplicates and
	// the commit holds them back rather than double-booking the money. ---
	status, body = h.doUpload(t, "/api/v1/statement-imports", "january-again.ofx",
		[]byte(testOFX), map[string]string{"bankAccountId": bank.AccountID.String()})
	require.Equal(t, http.StatusCreated, status, string(body))
	var again importResponse
	require.NoError(t, json.Unmarshal(body, &again))
	require.Len(t, again.StatementImports, 1)
	assert.Equal(t, 2, again.StatementImports[0].Import.DuplicateCount)

	status, body = h.do(t, http.MethodPost,
		"/api/v1/statement-imports/"+again.StatementImports[0].Import.ImportID+"/commit",
		map[string]any{"IncludeDuplicates": false}, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var second struct {
		Imported int `json:"Imported"`
		Skipped  int `json:"Skipped"`
	}
	require.NoError(t, json.Unmarshal(body, &second))
	assert.Equal(t, 0, second.Imported)
	assert.Equal(t, 2, second.Skipped)

	// --- Step 7: an import whose lines are already reconciled cannot be undone.
	status, body = h.do(t, http.MethodDelete, "/api/v1/statement-imports/"+imp.ImportID, nil, true)
	assert.Equal(t, http.StatusConflict, status, string(body))

	// The duplicate import has nothing reconciled, so it can be undone.
	status, body = h.do(t, http.MethodDelete, "/api/v1/statement-imports/"+again.StatementImports[0].Import.ImportID, nil, true)
	assert.Equal(t, http.StatusOK, status, string(body))
}

// TestHTTP_BankStatementIgnoreAndUnignore covers the non-terminal ignore state:
// Xero lets a user put a line back in the inbox.
func TestHTTP_BankStatementIgnoreAndUnignore(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)

	status, body := h.doUpload(t, "/api/v1/statement-imports", "january.ofx",
		[]byte(testOFX), map[string]string{"bankAccountId": bank.AccountID.String()})
	require.Equal(t, http.StatusCreated, status, string(body))
	var staged importResponse
	require.NoError(t, json.Unmarshal(body, &staged))
	impID := staged.StatementImports[0].Import.ImportID

	status, body = h.do(t, http.MethodPost, "/api/v1/statement-imports/"+impID+"/commit",
		map[string]any{}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	require.Len(t, lines, 2)
	id := lines[0].StatementLineID

	status, _ = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+id+"/ignore", nil, true)
	assert.Equal(t, http.StatusNoContent, status)
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 1,
		"an ignored line leaves the inbox")
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()+"&status=IGNORED"), 1)

	status, _ = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+id+"/unignore", nil, true)
	assert.Equal(t, http.StatusNoContent, status)
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 2)
}

// TestHTTP_BankStatementCreateMatchesExisting covers the reconcile screen's
// "match" tab: a line is linked to a transaction that already exists, and the
// transaction is reconciled by flipping its flag — not by posting a copy.
func TestHTTP_BankStatementCreateMatchesExisting(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	status, body := h.doUpload(t, "/api/v1/statement-imports", "january.ofx",
		[]byte(testOFX), map[string]string{"bankAccountId": bank.AccountID.String()})
	require.Equal(t, http.StatusCreated, status, string(body))
	var staged importResponse
	require.NoError(t, json.Unmarshal(body, &staged))
	status, body = h.do(t, http.MethodPost,
		"/api/v1/statement-imports/"+staged.StatementImports[0].Import.ImportID+"/commit",
		map[string]any{}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	// A transaction the user entered by hand, matching the credit line.
	status, body = h.do(t, http.MethodPost, "/api/v1/bank-transactions", map[string]any{
		"Type":          "RECEIVE",
		"BankAccountID": bank.AccountID.String(),
		"Date":          "2026-01-07T00:00:00Z",
		"Reference":     "Invoice 1042",
		"LineItems": []map[string]any{{
			"Description": "Invoice 1042", "Quantity": 1, "UnitAmount": 1200, "AccountCode": code,
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
	txID := created.BankTransactions[0].BankTransactionID

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	var creditLine string
	for _, l := range lines {
		if l.Amount == "1200" {
			creditLine = l.StatementLineID
		}
	}
	require.NotEmpty(t, creditLine)

	// The candidate search offers the hand-entered transaction.
	status, body = h.do(t, http.MethodGet, "/api/v1/statement-lines/"+creditLine+"/matches", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var matches struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &matches))
	require.NotEmpty(t, matches.BankTransactions)
	assert.Equal(t, txID, matches.BankTransactions[0].BankTransactionID,
		"the same-amount, same-date candidate must rank first")

	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+creditLine+"/match",
		map[string]any{"BankTransactionID": txID}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	// The line is out of the inbox and the transaction is reconciled — and it is
	// still the *same* transaction, so the money is not on the books twice.
	remaining := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	assert.Len(t, remaining, 1)
	status, body = h.do(t, http.MethodGet, "/api/v1/bank-transactions?bankAccountId="+bank.AccountID.String(), nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var all struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
			IsReconciled      bool   `json:"IsReconciled"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &all))
	require.Len(t, all.BankTransactions, 1, "matching must not create a second transaction")
	assert.True(t, all.BankTransactions[0].IsReconciled)
}

// The reconcile screen sends the rate and nothing else — the create payload has
// no field for a tax amount — so the server has to work the tax out of the rate.
// Without that the ledger takes a line that names a rate and holds a tax of
// 0.00, which is a line whose tax nothing records.
func TestHTTP_BankStatementCreateDerivesTheTaxFromTheRate(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	var spendLine statementLine
	for _, l := range listLines(t, h, "?bankAccountId="+bank.AccountID.String()) {
		if l.Amount == "-42.5" {
			spendLine = l
		}
	}
	require.NotEmpty(t, spendLine.StatementLineID, "the spend line must be in the inbox")

	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine.StatementLineID+"/create",
		map[string]any{"AccountCode": code, "TaxType": "INPUT"}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	var created struct {
		BankTransactions []struct {
			LineItems []struct {
				TaxType   string `json:"TaxType"`
				TaxAmount string `json:"TaxAmount"`
			} `json:"LineItems"`
			SubTotal string `json:"SubTotal"`
			TotalTax string `json:"TotalTax"`
			Total    string `json:"Total"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.BankTransactions, 1)
	tx := created.BankTransactions[0]
	require.Len(t, tx.LineItems, 1)
	assert.Equal(t, "INPUT", tx.LineItems[0].TaxType)

	// Tax on Purchases is 8.25%, and the statement's 42.50 is what left the
	// bank: the tax comes out of that figure, not on top of it, so the net is
	// 39.26 and the total is still the 42.50 the line states.
	assert.Equal(t, "3.24", trimMoney(tx.LineItems[0].TaxAmount), "the line's tax")
	assert.Equal(t, "39.26", trimMoney(tx.SubTotal), "the net, tax taken out of the gross")
	assert.Equal(t, "3.24", trimMoney(tx.TotalTax), "the tax")
	assert.Equal(t, "42.5", trimMoney(tx.Total), "the total is what the statement says")
}

// trimMoney drops the trailing zeroes the API prints decimal money with, so a
// test can state a figure the way a person reads it.
func trimMoney(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

// TestHTTP_BankStatementMatchRefusesBadPairs covers the two ways a hand-made
// match can be wrong. Matching asserts that a statement line and a bank
// transaction are the same money, so a transaction that is already reconciled
// cannot be claimed by a second line, and one whose amount disagrees is not a
// match at all. Both are refused at the API rather than left to the reconciler's
// arithmetic, and the candidate search does not offer the reconciled one.
func TestHTTP_BankStatementMatchRefusesBadPairs(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	status, body := h.doUpload(t, "/api/v1/statement-imports", "january.ofx",
		[]byte(testOFX), map[string]string{"bankAccountId": bank.AccountID.String()})
	require.Equal(t, http.StatusCreated, status, string(body))
	var staged importResponse
	require.NoError(t, json.Unmarshal(body, &staged))
	status, body = h.do(t, http.MethodPost,
		"/api/v1/statement-imports/"+staged.StatementImports[0].Import.ImportID+"/commit",
		map[string]any{}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	// One transaction for the credit line, and one whose amount matches
	// nothing on the statement.
	creditTx := createBankTransaction(t, h, bank.AccountID.String(), "RECEIVE", "2026-01-07T00:00:00Z", 1200, code)
	oddTx := createBankTransaction(t, h, bank.AccountID.String(), "SPEND", "2026-01-05T00:00:00Z", 17.25, code)

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	var creditLine, spendLine string
	for _, l := range lines {
		switch l.Amount {
		case "1200":
			creditLine = l.StatementLineID
		case "-42.5":
			spendLine = l.StatementLineID
		}
	}
	require.NotEmpty(t, creditLine)
	require.NotEmpty(t, spendLine)

	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+creditLine+"/match",
		map[string]any{"BankTransactionID": creditTx}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	// The reconciled transaction is no longer a candidate for the other line.
	status, body = h.do(t, http.MethodGet, "/api/v1/statement-lines/"+spendLine+"/matches", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var matches struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &matches))
	for _, c := range matches.BankTransactions {
		assert.NotEqual(t, creditTx, c.BankTransactionID,
			"a reconciled transaction must not be offered for matching")
	}

	// And matching to it anyway is refused rather than quietly double-booking.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/match",
		map[string]any{"BankTransactionID": creditTx}, true)
	require.Equal(t, http.StatusConflict, status, string(body))

	// An unreconciled transaction of the wrong size is not a match either: the
	// statement line is still in the inbox afterwards.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+spendLine+"/match",
		map[string]any{"BankTransactionID": oddTx}, true)
	require.Equal(t, http.StatusBadRequest, status, string(body))
	assert.Contains(t, string(body), "amounts do not match")

	status, body = h.do(t, http.MethodGet,
		"/api/v1/statement-lines?bankAccountId="+bank.AccountID.String()+"&unreconciled=true", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var inbox struct {
		StatementLines []statementLine `json:"StatementLines"`
	}
	require.NoError(t, json.Unmarshal(body, &inbox))
	require.Len(t, inbox.StatementLines, 1)
	assert.Equal(t, spendLine, inbox.StatementLines[0].StatementLineID)
}

// TestHTTP_BankStatementDiscuss covers Xero's "Discuss": notes are attached to
// the line, read back oldest first, and an empty note is refused rather than
// stored as a blank row in the conversation.
func TestHTTP_BankStatementDiscuss(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	require.NotEmpty(t, lines)
	line := lines[0].StatementLineID

	status, body := h.do(t, http.MethodGet, "/api/v1/statement-lines/"+line+"/comments", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var empty struct {
		Comments []struct {
			CommentID string `json:"CommentID"`
		} `json:"Comments"`
	}
	require.NoError(t, json.Unmarshal(body, &empty))
	assert.Empty(t, empty.Comments, "a fresh line has no discussion")

	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+line+"/comments",
		map[string]any{"Body": "  "}, true)
	require.Equal(t, http.StatusBadRequest, status, string(body))

	for _, text := range []string{"Is this the coffee subscription?", "Yes — coded to 620."} {
		status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+line+"/comments",
			map[string]any{"Body": text}, true)
		require.Equal(t, http.StatusCreated, status, string(body))
	}

	status, body = h.do(t, http.MethodGet, "/api/v1/statement-lines/"+line+"/comments", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var thread struct {
		Comments []struct {
			Body       string `json:"Body"`
			AuthorName string `json:"AuthorName"`
		} `json:"Comments"`
	}
	require.NoError(t, json.Unmarshal(body, &thread))
	require.Len(t, thread.Comments, 2)
	assert.Equal(t, "Is this the coffee subscription?", thread.Comments[0].Body,
		"the thread reads in the order it was written")
	assert.Equal(t, "Yes — coded to 620.", thread.Comments[1].Body)
	assert.NotEmpty(t, thread.Comments[0].AuthorName, "a note says who wrote it")

	// A line id from nowhere is a not-found, not an empty thread.
	status, _ = h.do(t, http.MethodGet,
		"/api/v1/statement-lines/11111111-1111-1111-1111-111111111111/comments", nil, true)
	assert.Equal(t, http.StatusNotFound, status)
}

// TestHTTP_BankStatementDeleteLine covers the other half of the per-row Options
// menu. A line nobody wants can go; a reconciled line cannot, because the
// ledger entry it explains would be left with no evidence behind it.
func TestHTTP_BankStatementDeleteLine(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)
	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	require.Len(t, lines, 2)
	var spend, credit string
	for _, l := range lines {
		if l.Amount == "-42.5" {
			spend = l.StatementLineID
		} else {
			credit = l.StatementLineID
		}
	}
	require.NotEmpty(t, spend)
	require.NotEmpty(t, credit)

	// Reconcile the credit line, then try to delete it.
	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/"+credit+"/create",
		map[string]any{"AccountCode": code}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	status, body = h.do(t, http.MethodDelete, "/api/v1/statement-lines/"+credit, nil, true)
	require.Equal(t, http.StatusConflict, status, string(body))

	status, body = h.do(t, http.MethodDelete, "/api/v1/statement-lines/"+spend, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	// The credit is reconciled now, so the inbox has to be asked for everything
	// to see that only the deleted line is gone.
	remaining := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+"&unreconciled=false")
	require.Len(t, remaining, 1)
	assert.Equal(t, credit, remaining[0].StatementLineID)

	status, _ = h.do(t, http.MethodDelete, "/api/v1/statement-lines/"+spend, nil, true)
	assert.Equal(t, http.StatusNotFound, status, "deleting twice is a not-found")
}

// TestHTTP_BankStatementAutoReconcileReport covers the banner: it counts the
// lines that arrived in the window and how many the button dealt with itself,
// carries the per-account setting, and — with the setting on — an import
// reconciles what it can without being asked.
func TestHTTP_BankStatementAutoReconcileReport(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)
	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))
	createBankTransaction(t, h, bank.AccountID.String(), "RECEIVE", "2026-01-07T00:00:00Z", 1200, code)

	report := autoReconcileReport(t, h, bank.AccountID.String())
	assert.Equal(t, 30, report.Days)
	assert.Equal(t, 2, report.Total)
	assert.Zero(t, report.AutoReconciled, "nothing has been auto-reconciled yet")
	assert.Equal(t, 2, report.UnreconciledLeft)
	assert.False(t, report.Enabled, "auto-reconcile is off until the user turns it on")

	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/auto-reconcile?bankAccountId="+bank.AccountID.String(), nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	report = autoReconcileReport(t, h, bank.AccountID.String())
	assert.Equal(t, 1, report.AutoReconciled, "the credit line agrees with the hand-entered transaction")
	assert.Equal(t, 1, report.UnreconciledLeft)

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+"&unreconciled=false")
	for _, l := range lines {
		if l.BankTransactionID == "" {
			assert.Empty(t, l.AutoReconciledAt, "a line nobody reconciled is not counted as auto-reconciled")
		} else {
			assert.NotEmpty(t, l.AutoReconciledAt, "the line the button matched is stamped as its work")
		}
	}

	// Turning the setting on runs it for the lines already waiting, and the
	// setting is readable from the account afterwards.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/auto-reconcile-settings",
		map[string]any{"BankAccountID": bank.AccountID.String(), "Enabled": true}, true)
	require.Equal(t, http.StatusOK, status, string(body))
	after := decodeReport(t, body)
	assert.True(t, after.Enabled)

	acc, err := h.repos.Accounts.GetByID(context.Background(), seedDemoOrgID, bank.AccountID)
	require.NoError(t, err)
	assert.True(t, acc.AutoReconcile, "the setting is stored on the bank account")

	status, _ = h.do(t, http.MethodPost, "/api/v1/statement-lines/auto-reconcile-settings",
		map[string]any{"BankAccountID": bank.AccountID.String(), "Enabled": false}, true)
	require.Equal(t, http.StatusOK, status)
}

// TestHTTP_BankStatementAutoReconcileOnImport covers the setting's other half:
// with it on, committing an import reconciles the lines it can straight away.
func TestHTTP_BankStatementAutoReconcileOnImport(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)
	createBankTransaction(t, h, bank.AccountID.String(), "RECEIVE", "2026-01-07T00:00:00Z", 1200, code)

	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/auto-reconcile-settings",
		map[string]any{"BankAccountID": bank.AccountID.String(), "Enabled": true}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	status, body = h.doUpload(t, "/api/v1/statement-imports", "january.ofx",
		[]byte(testOFX), map[string]string{"bankAccountId": bank.AccountID.String()})
	require.Equal(t, http.StatusCreated, status, string(body))
	var staged importResponse
	require.NoError(t, json.Unmarshal(body, &staged))
	status, body = h.do(t, http.MethodPost,
		"/api/v1/statement-imports/"+staged.StatementImports[0].Import.ImportID+"/commit",
		map[string]any{}, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var committed struct {
		AutoMatched int `json:"AutoMatched"`
	}
	require.NoError(t, json.Unmarshal(body, &committed))
	assert.Equal(t, 1, committed.AutoMatched, "the import reconciles what it can when the account asks it to")

	report := autoReconcileReport(t, h, bank.AccountID.String())
	assert.Equal(t, 1, report.AutoReconciled)
	assert.Equal(t, 1, report.UnreconciledLeft)
}

// TestHTTP_BankStatementAmountRange covers the amount half of Xero's Filter.
// The range compares magnitude, so a debit of 42.50 is inside "10 to 50" even
// though its signed amount is not.
func TestHTTP_BankStatementAmountRange(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	inRange := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+"&minAmount=10&maxAmount=50")
	require.Len(t, inRange, 1)
	assert.Equal(t, "-42.5", inRange[0].Amount)

	onlyCredit := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+"&minAmount=100")
	require.Len(t, onlyCredit, 1)
	assert.Equal(t, "1200", onlyCredit[0].Amount)

	none := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+"&maxAmount=1")
	assert.Empty(t, none)

	status, body := h.do(t, http.MethodGet,
		"/api/v1/statement-lines?bankAccountId="+bank.AccountID.String()+"&minAmount=nonsense", nil, true)
	assert.Equal(t, http.StatusBadRequest, status, string(body))
}

func autoReconcileReport(t *testing.T, h *appHarness, bankAccountID string) models.AutoReconcileReport {
	t.Helper()
	status, body := h.do(t, http.MethodGet,
		"/api/v1/statement-lines/auto-reconcile?bankAccountId="+bankAccountID, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	return decodeReport(t, body)
}

// decodeReport unwraps the `{AutoReconcile: {...}}` body the report endpoints
// answer with.
func decodeReport(t *testing.T, body []byte) models.AutoReconcileReport {
	t.Helper()
	var out struct {
		AutoReconcile models.AutoReconcileReport
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.AutoReconcile
}

// TestHTTP_BankStatementCSVWizardMapping covers the CSV half of the wizard: the
// mapping the user confirms is re-applied on a re-upload, over a file whose
// columns we would not have guessed the same way.
func TestHTTP_BankStatementCSVWizardMapping(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)

	csv := "When,What,Money out,Money in,Balance\n" +
		"03/02/2026,Card payment,12.34,,988.00\n" +
		"13/02/2026,Client receipt,,500.00,1488.00\n"

	status, body := h.doUpload(t, "/api/v1/statement-imports", "february.csv",
		[]byte(csv), map[string]string{"bankAccountId": bank.AccountID.String()})
	require.Equal(t, http.StatusCreated, status, string(body))
	var staged importResponse
	require.NoError(t, json.Unmarshal(body, &staged))
	require.Len(t, staged.StatementImports, 1)
	imp := staged.StatementImports[0].Import
	assert.Equal(t, "CSV", imp.Format)
	assert.Equal(t, 2, imp.LineCount, "debit and credit columns must both parse")
	require.Len(t, staged.StatementImports[0].Preview, 2)
	assert.Equal(t, "2026-02-03", staged.StatementImports[0].Preview[0].Date,
		"13/02/2026 can only be day-first, which settles the whole column")
	assert.Equal(t, "-12.34", staged.StatementImports[0].Preview[0].Amount)
	assert.Equal(t, "500", staged.StatementImports[0].Preview[1].Amount)
	// The file declares no opening balance, but it prints a running balance: the
	// balance before the first line is that line's balance less its amount.
	assert.Equal(t, "1000.34", staged.StatementImports[0].Import.OpeningBalance)
	assert.Equal(t, "1488", staged.StatementImports[0].Import.ClosingBalance)

	// Step 2 again, with a mapping the user corrected. The response has to carry
	// the columns back or the wizard's mapping table goes blank on the redraw.
	corrected := map[string]any{
		"HasHeader":        true,
		"SkipRows":         0,
		"AmountMode":       "DEBIT_CREDIT",
		"Date":             "When",
		"Debit":            "Money out",
		"Credit":           "Money in",
		"Payee":            "Balance",
		"Balance":          "Balance",
		"DecimalSeparator": ".",
	}
	raw, err := json.Marshal(corrected)
	require.NoError(t, err)
	status, body = h.doUpload(t, "/api/v1/statement-imports/"+imp.ImportID+"/remap", "february.csv",
		[]byte(csv), map[string]string{"mapping": string(raw)})
	require.Equal(t, http.StatusOK, status, string(body))
	var remapped struct {
		StatementImports []struct {
			Columns []string       `json:"Columns"`
			Mapping map[string]any `json:"Mapping"`
			Preview []struct {
				Date   string `json:"Date"`
				Amount string `json:"Amount"`
				Payee  string `json:"Payee"`
			} `json:"Preview"`
		} `json:"StatementImports"`
	}
	require.NoError(t, json.Unmarshal(body, &remapped))
	require.Len(t, remapped.StatementImports, 1)
	assert.Equal(t, []string{"When", "What", "Money out", "Money in", "Balance"},
		remapped.StatementImports[0].Columns)
	assert.Equal(t, "Balance", remapped.StatementImports[0].Mapping["Payee"])
	require.Len(t, remapped.StatementImports[0].Preview, 2)
	assert.Equal(t, "988.00", remapped.StatementImports[0].Preview[0].Payee,
		"the rows are re-read with the mapping the user confirmed, not the guessed one")
}

// TestHTTP_BankStatementReconcilePeriods covers the Reconcile period tab, whose
// whole purpose is to stop two overlapping periods from being declared.
func TestHTTP_BankStatementReconcilePeriods(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)

	status, body := h.do(t, http.MethodPost, "/api/v1/reconcile-periods", map[string]any{
		"BankAccountID": bank.AccountID.String(),
		"StartDate":     "2026-01-01",
		"EndDate":       "2026-01-31",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		ReconcilePeriods []struct {
			PeriodID string `json:"PeriodID"`
		} `json:"ReconcilePeriods"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.ReconcilePeriods, 1)
	periodID := created.ReconcilePeriods[0].PeriodID

	// An identical period is refused.
	status, _ = h.do(t, http.MethodPost, "/api/v1/reconcile-periods", map[string]any{
		"BankAccountID": bank.AccountID.String(),
		"StartDate":     "2026-01-01",
		"EndDate":       "2026-01-31",
	}, true)
	assert.Equal(t, http.StatusConflict, status)

	// A backwards range is refused before it reaches the database.
	status, _ = h.do(t, http.MethodPost, "/api/v1/reconcile-periods", map[string]any{
		"BankAccountID": bank.AccountID.String(),
		"StartDate":     "2026-03-01",
		"EndDate":       "2026-02-01",
	}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	status, body = h.do(t, http.MethodGet, "/api/v1/reconcile-periods?bankAccountId="+bank.AccountID.String(), nil, true)
	require.Equal(t, http.StatusOK, status, string(body))

	status, _ = h.do(t, http.MethodDelete, "/api/v1/reconcile-periods/"+periodID, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
}

// TestHTTP_BankStatementApplyRule checks that the rule engine is reachable from
// the inbox: a saved rule suggests the coding for a matching line.
func TestHTTP_BankStatementApplyRule(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	status, body := h.do(t, http.MethodPost, "/api/v1/bank-rules", map[string]any{
		"Name":     "Starbucks",
		"RuleType": "SPEND",
		"IsActive": true,
		"Definition": map[string]any{
			"MatchMode": "ALL",
			"Conditions": []map[string]any{
				{"Field": "PAYEE", "Operator": "contains", "Value": "starbucks"},
			},
			"PercentLines": []map[string]any{{"AccountID": code, "Percent": 100}},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	status, body = h.doUpload(t, "/api/v1/statement-imports", "january.ofx",
		[]byte(testOFX), map[string]string{"bankAccountId": bank.AccountID.String()})
	require.Equal(t, http.StatusCreated, status, string(body))
	var staged importResponse
	require.NoError(t, json.Unmarshal(body, &staged))
	status, body = h.do(t, http.MethodPost,
		"/api/v1/statement-imports/"+staged.StatementImports[0].Import.ImportID+"/commit",
		map[string]any{}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/apply-rule", map[string]any{
		"BankAccountID": bank.AccountID.String(),
	}, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var applied struct {
		StatementLines []struct {
			Payee       string `json:"Payee"`
			Suggestions []struct {
				RuleName string `json:"RuleName"`
			} `json:"Suggestions"`
		} `json:"StatementLines"`
	}
	require.NoError(t, json.Unmarshal(body, &applied))
	require.Len(t, applied.StatementLines, 2)
	var suggested int
	for _, l := range applied.StatementLines {
		if len(l.Suggestions) > 0 {
			suggested++
			assert.Equal(t, "Starbucks", l.Suggestions[0].RuleName)
		}
	}
	assert.Equal(t, 1, suggested, "only the Starbucks line matches the rule")

	// Reordering is how a user resolves two rules that both match.
	status, body = h.do(t, http.MethodPut, "/api/v1/bank-rules/order", map[string]any{
		"BankRuleIDs": []string{uuid.Nil.String()},
	}, true)
	assert.Equal(t, http.StatusOK, status, string(body), "an unknown id simply matches nothing")
}

// TestHTTP_BankStatementTransferDirection pins the one thing a transfer can get
// wrong: which way the money went. A debit line on the reconciled account sent
// money out, so that account is the source; a credit line received money, so
// the account the user picked is.
func TestHTTP_BankStatementTransferDirection(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)

	status, body := h.do(t, http.MethodPost, "/api/v1/accounts", map[string]any{
		"Code": "0910", "Name": "Savings", "Type": "BANK", "Status": "ACTIVE",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		Accounts []struct {
			AccountID string `json:"AccountID"`
		} `json:"Accounts"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.Accounts, 1)
	other := created.Accounts[0].AccountID

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	var debitLine, creditLine string
	for _, l := range listLines(t, h, "?bankAccountId="+bank.AccountID.String()) {
		switch l.Amount {
		case "-42.5":
			debitLine = l.StatementLineID
		case "1200":
			creditLine = l.StatementLineID
		}
	}
	require.NotEmpty(t, debitLine)
	require.NotEmpty(t, creditLine)

	type transferResponse struct {
		BankTransfers []struct {
			FromBankAccountID string `json:"FromBankAccountID"`
			ToBankAccountID   string `json:"ToBankAccountID"`
			Amount            string `json:"Amount"`
		} `json:"BankTransfers"`
	}

	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+debitLine+"/transfer",
		map[string]any{"ToBankAccountID": other, "Reference": "to savings"}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var out transferResponse
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.BankTransfers, 1)
	assert.Equal(t, bank.AccountID.String(), out.BankTransfers[0].FromBankAccountID,
		"a debit line moved money out of the reconciled account")
	assert.Equal(t, other, out.BankTransfers[0].ToBankAccountID)
	assert.Equal(t, "42.5", out.BankTransfers[0].Amount)

	// The credit line is the mirror image: the money arrived here from elsewhere.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/"+creditLine+"/transfer",
		map[string]any{"ToBankAccountID": other}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.BankTransfers, 1)
	assert.Equal(t, other, out.BankTransfers[0].FromBankAccountID,
		"a credit line received money from the account the user picked")
	assert.Equal(t, bank.AccountID.String(), out.BankTransfers[0].ToBankAccountID)

	assert.Empty(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()),
		"both lines are out of the inbox once transferred")
}

// TestHTTP_BankStatementAutoReconcile covers the "Ok, let's reconcile" button:
// it matches only the lines that agree exactly on date and amount, and leaves
// everything it is unsure about alone.
func TestHTTP_BankStatementAutoReconcile(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	// One of the two lines gets a matching unreconciled transaction; the other
	// one does not, and must be left in the inbox.
	status, body := h.do(t, http.MethodPost, "/api/v1/bank-transactions", map[string]any{
		"Type":          "SPEND",
		"BankAccountID": bank.AccountID.String(),
		"Date":          "2026-01-05T00:00:00Z",
		"Reference":     "Coffee",
		"LineItems": []map[string]any{{
			"Description": "Coffee", "Quantity": 1, "UnitAmount": 42.50, "AccountCode": code,
		}},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	status, body = h.do(t, http.MethodPost,
		"/api/v1/statement-lines/auto-reconcile?bankAccountId="+bank.AccountID.String(), nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var res struct {
		Matched   int `json:"Matched"`
		Scanned   int `json:"Scanned"`
		Remaining int `json:"Remaining"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	assert.Equal(t, 2, res.Scanned)
	assert.Equal(t, 1, res.Matched, "only the line with a same-day, same-amount transaction matches")
	assert.Equal(t, 1, res.Remaining)

	remaining := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	require.Len(t, remaining, 1)
	assert.Equal(t, "1200", remaining[0].Amount, "the unmatched line is the one still waiting")
}

// TestHTTP_BankStatementAutoReconcileRespectsDirection pins the direction half
// of "exact match": a transaction of the same size and date only reconciles a
// line that moves the money the same way, so an incoming refund cannot be
// booked against an outgoing card payment.
func TestHTTP_BankStatementAutoReconcileRespectsDirection(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	// Same date and amount as the debit line, but money in rather than out.
	status, body := h.do(t, http.MethodPost, "/api/v1/bank-transactions", map[string]any{
		"Type":          "RECEIVE",
		"BankAccountID": bank.AccountID.String(),
		"Date":          "2026-01-05T00:00:00Z",
		"Reference":     "Refund",
		"LineItems": []map[string]any{{
			"Description": "Refund", "Quantity": 1, "UnitAmount": 42.50, "AccountCode": code,
		}},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	status, body = h.do(t, http.MethodPost,
		"/api/v1/statement-lines/auto-reconcile?bankAccountId="+bank.AccountID.String(), nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var res struct {
		Matched   int `json:"Matched"`
		Remaining int `json:"Remaining"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	assert.Zero(t, res.Matched, "an incoming refund must not reconcile an outgoing payment")
	assert.Equal(t, 2, res.Remaining)
}

// TestHTTP_BankStatementLineIsClaimedOnce covers the claim guarding the inbox: a
// double-click or two tabs submitting the same line must post one transaction,
// and the loser is told the line has already been reconciled.
func TestHTTP_BankStatementLineIsClaimedOnce(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	require.Len(t, lines, 2)
	path := "/api/v1/statement-lines/" + lines[0].StatementLineID + "/create"
	body := map[string]any{"AccountCode": code}

	status, resp := h.do(t, http.MethodPost, path, body, true)
	require.Equal(t, http.StatusCreated, status, string(resp))
	status, resp = h.do(t, http.MethodPost, path, body, true)
	assert.Equal(t, http.StatusConflict, status, string(resp))

	status, resp = h.do(t, http.MethodGet, "/api/v1/bank-transactions?bankAccountId="+bank.AccountID.String(), nil, true)
	require.Equal(t, http.StatusOK, status, string(resp))
	var all struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(resp, &all))
	assert.Len(t, all.BankTransactions, 1, "the losing submission must not post a second transaction")
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 1)
}

// TestHTTP_BankStatementBulkIgnore covers the ignore action the reconcile and
// cash-coding screens use on a selection.
func TestHTTP_BankStatementBulkIgnore(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))

	lines := listLines(t, h, "?bankAccountId="+bank.AccountID.String())
	require.Len(t, lines, 2)

	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/bulk", map[string]any{
		"StatementLineIDs": []string{lines[0].StatementLineID},
		"Action":           "IGNORE",
	}, true)
	require.Equal(t, http.StatusNoContent, status, string(body))
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 1)

	// An ignored line is still on the account, so it comes back when the filter
	// asks for everything.
	all := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+"&unreconciled=false")
	require.Len(t, all, 2)
	var ignored int
	for _, l := range all {
		if l.Status == "IGNORED" {
			ignored++
		}
	}
	assert.Equal(t, 1, ignored)

	// Unignoring puts it back in the inbox.
	status, body = h.do(t, http.MethodPost, "/api/v1/statement-lines/bulk", map[string]any{
		"StatementLineIDs": []string{lines[0].StatementLineID},
		"Action":           "UNIGNORE",
	}, true)
	require.Equal(t, http.StatusNoContent, status, string(body))
	assert.Len(t, listLines(t, h, "?bankAccountId="+bank.AccountID.String()), 2)
}

// TestHTTP_BankStatementSearch covers the search box above the reconcile list.
// Xero's looks over the payee, the reference, the description and the amount,
// and the amount is what people type most: it is the number printed on the
// statement in front of them.
func TestHTTP_BankStatementSearch(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))
	base := "?bankAccountId=" + bank.AccountID.String()

	// By payee, in lower case and with the spacing the bank did not use.
	byPayee := listLines(t, h, base+"&search=starbucks+store")
	require.Len(t, byPayee, 1)
	assert.Equal(t, "STARBUCKS STORE 1234", byPayee[0].Payee)

	// By amount. The line is stored as -42.50, and nobody searching for the
	// coffee they bought types the minus sign.
	byAmount := listLines(t, h, base+"&search=42.50")
	require.Len(t, byAmount, 1)
	assert.Equal(t, "STARBUCKS STORE 1234", byAmount[0].Payee)

	// By the reference the bank sent in the memo.
	byReference := listLines(t, h, base+"&search=invoice+1042")
	require.Len(t, byReference, 1)
	assert.Equal(t, "ACME CLIENT PAYMENT", byReference[0].Payee)

	// A term no line carries narrows to nothing rather than to everything.
	assert.Empty(t, listLines(t, h, base+"&search=zzzznothing"))
}

// TestHTTP_BankStatementLineCarriesItsCoding covers the link between a
// statement line and the transaction it became. Xero shows the coding in the
// line's own Code column on the Bank statements tab, so the line has to be able
// to answer "what did I turn into?" directly — looking it up through the
// account's transaction list would leave the column blank on any account with
// more transactions than one page.
func TestHTTP_BankStatementLineCarriesItsCoding(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)
	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))
	base := "?bankAccountId=" + bank.AccountID.String() + "&unreconciled=false"

	// A line nobody has dealt with has no coding to show.
	for _, l := range listLines(t, h, base) {
		assert.Empty(t, l.CodedAccountCode)
	}

	var spendID string
	for _, l := range listLines(t, h, "?bankAccountId="+bank.AccountID.String()) {
		if l.Amount == "-42.5" {
			spendID = l.StatementLineID
		}
	}
	require.NotEmpty(t, spendID)
	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/cash-code", map[string]any{
		"Rows": []map[string]any{{"StatementLineID": spendID, "AccountCode": code}},
	}, true)
	require.Equal(t, http.StatusOK, status, string(body))

	var coffee statementLine
	for _, l := range listLines(t, h, base) {
		if l.Payee == "STARBUCKS STORE 1234" {
			coffee = l
		}
	}
	require.NotEmpty(t, coffee.BankTransactionID, "the line must point at what it became")
	assert.Equal(t, code, coffee.CodedAccountCode)
	assert.NotEmpty(t, coffee.CodedAccountName, "the column names the account, not just its code")

	// The other side of the same link: a transaction answers with its own
	// coding, which is what the Account transactions tab shows.
	status, body = h.do(t, http.MethodGet,
		"/api/v1/bank-transactions?bankAccountId="+bank.AccountID.String(), nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var txs struct {
		BankTransactions []struct {
			BankTransactionID string `json:"BankTransactionID"`
			LineItems         []struct {
				AccountCode string `json:"AccountCode"`
			} `json:"LineItems"`
		} `json:"BankTransactions"`
	}
	require.NoError(t, json.Unmarshal(body, &txs))
	require.Len(t, txs.BankTransactions, 1)
	require.Len(t, txs.BankTransactions[0].LineItems, 1,
		"the transaction list carries the line items the account column is built from")
	assert.Equal(t, code, txs.BankTransactions[0].LineItems[0].AccountCode)
	assert.Equal(t, coffee.BankTransactionID, txs.BankTransactions[0].BankTransactionID)
}

var fiberTestConfig = fiber.TestConfig{Timeout: -1}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return b
}

// February's export of the same account. The coffee line is the same payee the
// way the next export punctuated it — banks are not consistent about spacing or
// case between months, and neither is anyone's typing.
const testOFXNextMonth = `OFXHEADER:100
DATA:OFXSGML
VERSION:102
SECURITY:NONE
ENCODING:USASCII

<OFX>
<BANKMSGSRSV1>
<STMTTRNRS>
<STMTRS>
<CURDEF>USD
<BANKACCTFROM>
<BANKID>121000248
<ACCTID>132435465
</BANKACCTFROM>
<BANKTRANLIST>
<DTSTART>20260201
<DTEND>20260228
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20260205
<TRNAMT>-42.50
<FITID>OFX-3
<NAME>Starbucks  store 1234
<MEMO>Coffee
</STMTTRN>
<STMTTRN>
<TRNTYPE>DEBIT
<DTPOSTED>20260206
<TRNAMT>-18.00
<FITID>OFX-4
<NAME>SOMEWHERE NEW
<MEMO>Lunch
</STMTTRN>
</BANKTRANLIST>
<LEDGERBAL>
<BALAMT>1097.50
<DTASOF>20260228
</LEDGERBAL>
</STMTRS>
</STMTTRNRS>
</BANKMSGSRSV1>
</OFX>`

// previousEntryOf pulls the suggestion out of a list response by payee.
type suggestedLine struct {
	StatementLineID string `json:"StatementLineID"`
	Payee           string `json:"Payee"`
	PreviousEntry   *struct {
		Payee       string `json:"Payee"`
		MatchCount  int    `json:"MatchCount"`
		AccountCode string `json:"AccountCode"`
		AccountName string `json:"AccountName"`
		TaxType     string `json:"TaxType"`
		Description string `json:"Description"`
		Reference   string `json:"Reference"`
	} `json:"PreviousEntry"`
}

func linesWithSuggestions(t *testing.T, h *appHarness, query string) []suggestedLine {
	t.Helper()
	status, body := h.do(t, http.MethodGet, "/api/v1/statement-lines"+query, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var out struct {
		StatementLines []suggestedLine `json:"StatementLines"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.StatementLines
}

// TestHTTP_BankStatementSuggestPreviousEntries covers Xero's "Suggest previous
// entries": once a payee has been coded on an account, the next line from the
// same payee arrives with that coding already suggested.
func TestHTTP_BankStatementSuggestPreviousEntries(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)
	code := firstAccountCode(t, h, bank.Code)

	stageAndCommit(t, h, bank.AccountID.String(), "january.ofx", []byte(testOFX))
	var coffee string
	for _, l := range listLines(t, h, "?bankAccountId="+bank.AccountID.String()) {
		if l.Payee == "STARBUCKS STORE 1234" {
			coffee = l.StatementLineID
		}
	}
	require.NotEmpty(t, coffee, "the January coffee line must be in the inbox")

	status, body := h.do(t, http.MethodPost, "/api/v1/statement-lines/"+coffee+"/create",
		map[string]any{
			"AccountCode": code,
			"TaxType":     "NONE",
			"Description": "Team coffee",
			"Reference":   "Coffee run",
		}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	// Nothing is suggested from a coding the account has never seen.
	before := linesWithSuggestions(t, h, "?bankAccountId="+bank.AccountID.String()+"&previousEntries=true")
	require.Len(t, before, 1)
	assert.Nil(t, before[0].PreviousEntry, "an uncoded payee has no previous entry to suggest")

	stageAndCommit(t, h, bank.AccountID.String(), "february.ofx", []byte(testOFXNextMonth))

	after := linesWithSuggestions(t, h, "?bankAccountId="+bank.AccountID.String()+"&previousEntries=true")
	byPayee := map[string]suggestedLine{}
	for _, l := range after {
		byPayee[l.Payee] = l
	}
	require.Contains(t, byPayee, "Starbucks  store 1234", "the February line is in the inbox")
	require.Contains(t, byPayee, "SOMEWHERE NEW")

	seen := byPayee["Starbucks  store 1234"]
	require.NotNil(t, seen.PreviousEntry, "the payee was coded last month, so it is suggested")
	assert.Equal(t, code, seen.PreviousEntry.AccountCode)
	assert.NotEmpty(t, seen.PreviousEntry.AccountName,
		"the hint names the account, not just its code")
	assert.Equal(t, "Team coffee", seen.PreviousEntry.Description)
	assert.Equal(t, "Coffee run", seen.PreviousEntry.Reference)
	assert.Equal(t, "NONE", seen.PreviousEntry.TaxType)
	assert.Equal(t, 1, seen.PreviousEntry.MatchCount)
	assert.Equal(t, "STARBUCKS STORE 1234", seen.PreviousEntry.Payee,
		"the suggestion is named after the entry it came from, not this line")

	assert.Nil(t, byPayee["SOMEWHERE NEW"].PreviousEntry,
		"a payee with no history gets no suggestion")

	// The setting is what turns it on: with it off the field is simply absent.
	off := linesWithSuggestions(t, h, "?bankAccountId="+bank.AccountID.String())
	for _, l := range off {
		assert.Nil(t, l.PreviousEntry, "no suggestion unless the caller asked for one")
	}

	// Opening one line always carries the suggestion — that is the panel the
	// Create button is about to fill in.
	status, body = h.do(t, http.MethodGet, "/api/v1/statement-lines/"+seen.StatementLineID, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var one struct {
		StatementLines []suggestedLine `json:"StatementLines"`
	}
	require.NoError(t, json.Unmarshal(body, &one))
	require.Len(t, one.StatementLines, 1)
	require.NotNil(t, one.StatementLines[0].PreviousEntry)
	assert.Equal(t, code, one.StatementLines[0].PreviousEntry.AccountCode)
}

// statementCSV builds a CSV statement for the import wizard: a header the
// detector understands, then one row per line. A row with an empty balance is
// one the bank printed no balance against, which is the case the running total
// has to cover.
func statementCSV(rows [][4]string) []byte {
	var b strings.Builder
	b.WriteString("Date,Payee,Amount,Balance\n")
	for _, r := range rows {
		b.WriteString(strings.Join(r[:], ","))
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// TestHTTP_BankStatementLineRunningBalance pins the Balance column to the
// account's own ledger rather than to the page it was read on. Three things
// have to hold, and the old client-side running total held none of them: the
// figure is the cumulative sum from the account's opening balance, a balance
// the bank itself printed wins over that sum, and the same line reports the
// same figure whichever page it is fetched on.
func TestHTTP_BankStatementLineRunningBalance(t *testing.T) {
	h := newHarness(t)
	bank := newStatementAccount(t, h)

	// The account's first statement: one line, with the balance the bank
	// printed after it. Subtracting the line leaves the balance the account
	// opened with, which the import records — 900.00 - (-100.00) = 1000.00.
	stageAndCommit(t, h, bank.AccountID.String(), "opening.csv", statementCSV([][4]string{
		{"2026-01-05", "Opening Coffee", "-100.00", "900.00"},
	}))

	// The statement being reconciled. Its first line carries a balance the bank
	// printed that deliberately disagrees with the account's own arithmetic, so
	// the two answers cannot be confused; the other two carry none, so their
	// figures can only have been computed.
	stageAndCommit(t, h, bank.AccountID.String(), "february.csv", statementCSV([][4]string{
		{"2026-02-01", "Disagreeing", "-50.00", "777.77"},
		{"2026-02-02", "Deposit", "250.00", ""},
		{"2026-02-03", "Petty cash", "-25.00", ""},
	}))

	all := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+"&unreconciled=false&pageSize=200")
	require.Len(t, all, 4)

	byPayee := map[string]statementLine{}
	for _, l := range all {
		assert.NotEmpty(t, l.Balance, "every line carries a balance, including %s", l.Payee)
		byPayee[l.Payee] = l
	}

	// (a) The running balance is the cumulative sum from the account's opening:
	// 1000.00 - 100.00 = 900.00 on the first line, then + (-50.00 + 250.00) =
	// 1100.00, then - 25.00 = 1075.00. The opening comes from the first
	// statement's import, not from the first line looked at.
	assert.Equal(t, "900", byPayee["Opening Coffee"].Balance)
	assert.Equal(t, "1100", byPayee["Deposit"].Balance)
	assert.Equal(t, "1075", byPayee["Petty cash"].Balance)

	// (b) A balance the bank itself printed wins over the computed one. The
	// arithmetic puts this line at 850.00 — the next line's figure less the
	// amount that moved it there — and the statement says 777.77. Xero shows
	// the statement's number, so the two must not agree.
	// The line above carries no printed balance, so it is only priced if the
	// server computed one; assert rather than require so that (c) below still
	// reports on the code this test is supposed to fail against.
	deposit, err := strconv.Atoi(byPayee["Deposit"].Balance)
	if assert.NoError(t, err, "the line above a priced line must itself be priced") {
		assert.Equal(t, 850, deposit-250, "by arithmetic the priced line sits at 850.00")
	}
	assert.Equal(t, "777.77", byPayee["Disagreeing"].Balance,
		"the balance the bank printed replaces the computed one")
	assert.NotEqual(t, byPayee["Disagreeing"].Balance, byPayee["Deposit"].Balance)

	// (c) The value does not depend on the page it is read from. Reading every
	// line alone on its own page must reproduce the figures above exactly —
	// this is the defect: the old fallback summed whatever rows the caller
	// happened to hold, so a one-line page reported the line's own amount.
	for i := range all {
		solo := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+
			"&unreconciled=false&pageSize=1&page="+strconv.Itoa(i+1))
		require.Len(t, solo, 1)
		assert.Equal(t, all[i].StatementLineID, solo[0].StatementLineID)
		assert.NotEmpty(t, solo[0].Balance,
			"a one-line page must still price the line it holds, %s", solo[0].Payee)
		assert.Equal(t, all[i].Balance, solo[0].Balance,
			"line %s changed its balance when paged on its own", all[i].Payee)
	}

	// A filter is not a page either: the same line keeps its number when the
	// caller narrows the inbox around it.
	filtered := listLines(t, h, "?bankAccountId="+bank.AccountID.String()+
		"&unreconciled=false&fromDate=2026-02-03")
	require.Len(t, filtered, 1)
	assert.Equal(t, byPayee["Petty cash"].Balance, filtered[0].Balance,
		"narrowing the inbox must not renumber the lines in it")
}
