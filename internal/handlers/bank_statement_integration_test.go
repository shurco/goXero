package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

func firstBankAccount(t *testing.T, h *appHarness) models.Account {
	t.Helper()
	list, err := h.repos.Accounts.List(context.Background(), seedDemoOrgID, repository.AccountFilter{Type: "BANK"})
	require.NoError(t, err)
	require.NotEmpty(t, list, "the demo fixture must seed a BANK account")
	return list[0]
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
	Source            string `json:"Source"`
	Status            string `json:"Status"`
	Amount            string `json:"Amount"`
	Payee             string `json:"Payee"`
	BankTransactionID string `json:"BankTransactionID"`
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
	bank := firstBankAccount(t, h)
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
	bank := firstBankAccount(t, h)

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
	bank := firstBankAccount(t, h)
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

// TestHTTP_BankStatementCSVWizardMapping covers the CSV half of the wizard: the
// mapping the user confirms is re-applied on a re-upload, over a file whose
// columns we would not have guessed the same way.
func TestHTTP_BankStatementCSVWizardMapping(t *testing.T) {
	h := newHarness(t)
	bank := firstBankAccount(t, h)

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
	bank := firstBankAccount(t, h)

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
	bank := firstBankAccount(t, h)
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
	bank := firstBankAccount(t, h)

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
	bank := firstBankAccount(t, h)
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
	bank := firstBankAccount(t, h)
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
	bank := firstBankAccount(t, h)
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
	bank := firstBankAccount(t, h)

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

var fiberTestConfig = fiber.TestConfig{Timeout: -1}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return b
}
