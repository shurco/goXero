package handlers_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Cash Summary is a cash-basis view: the movement of the bank accounts
// explained by what the money was *coded* to. Where it was coded to Accounts
// Receivable or Accounts Payable, what the money was coded to is not the
// control account — it is the coding on the document the cash settled, and
// these tests pin that look-through, the tax the sections carry, and the
// reporting of a movement the ledger links no document to at all.

// cashSummaryWindow is the window the tests below read; every posting they make
// falls inside it, so no assertion has to reason about a boundary.
const cashSummaryWindow = "fromDate=2026-02-01&toDate=2026-02-28"

// unattributedMark is the text a row carrying cash the ledger links no document
// to is labelled with, spelled out here rather than reached for, because it is
// what a reader of the report sees.
const unattributedMark = "(not attributed to a document)"

func cashSummaryReport(t *testing.T, h *appHarness) xeroReportEnvelope {
	t.Helper()
	return fetchReport(t, h, "/api/v1/reports/cash-summary?"+cashSummaryWindow)
}

// cashSummaryNetMovementAsPrinted adds the rows a reader can see: Total Income,
// less Total Expenses, plus Total Other Cash Movements, plus Net Tax Movements.
func cashSummaryNetMovementAsPrinted(t *testing.T, env xeroReportEnvelope) decimalLike {
	t.Helper()
	return add(
		sub(
			add(valueByLabel(t, env, "Total Income"), valueByLabel(t, env, "Total Other Cash Movements")),
			valueByLabel(t, env, "Total Expenses"),
		),
		valueByLabel(t, env, "Net Tax Movements"),
	)
}

// accountIDForCode is the id the payment API wants for the bank account the
// money moved through.
func accountIDForCode(t *testing.T, h *appHarness, code string) string {
	t.Helper()
	var id string
	require.NoError(t, h.repos.Pool.QueryRow(context.Background(),
		`SELECT account_id::text FROM accounts WHERE organisation_id = $1 AND code = $2`,
		seedDemoOrgID, code).Scan(&id))
	return id
}

// postInvoiceWithLines posts an authorised invoice whose line items the test
// codes, so it controls the accounts the cash will be attributed to.
func postInvoiceWithLines(t *testing.T, h *appHarness, invType, contactID, date, dueDate string, lines ...map[string]any) string {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "/api/v1/invoices", map[string]any{
		"Type":            invType,
		"Status":          "AUTHORISED",
		"ContactID":       contactID,
		"Date":            date,
		"DueDate":         dueDate,
		"LineAmountTypes": "Exclusive",
		"LineItems":       lines,
	}, true)
	require.Equalf(t, http.StatusCreated, status, "%s => %s", invType, string(body))
	return invoiceID(t, body)
}

// postPaymentForInvoice pays a document through a bank account, which is what
// makes the payment a movement of cash.
func postPaymentForInvoice(t *testing.T, h *appHarness, invType, invoiceID, bankCode, amount, date string) {
	t.Helper()
	paymentType := map[string]string{"ACCREC": "ACCRECPAYMENT", "ACCPAY": "ACCPAYPAYMENT"}[invType]
	require.NotEmptyf(t, paymentType, "no payment type for invoice type %s", invType)
	status, body := h.do(t, http.MethodPost, "/api/v1/payments", map[string]any{
		"InvoiceID":   invoiceID,
		"AccountID":   accountIDForCode(t, h, bankCode),
		"PaymentType": paymentType,
		"Amount":      amount,
		"Date":        date,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
}

// TestHTTP_Reports_CashSummaryLooksThroughReceivablesToTheInvoice is the
// regression test for a customer receipt that was reported on the control
// account: the cash was credited to Accounts Receivable, so the old report
// printed 610's own movement — the whole receipt, as one line of Plus Other
// Cash Movements — and said nothing about what the customer bought.
//
// The movement belongs to the invoice's own line items instead, each to the
// account that line coded: one sale is 100 of Sales and another 50 of Other
// Revenue, not 150 of Accounts Receivable.
func TestHTTP_Reports_CashSummaryLooksThroughReceivablesToTheInvoice(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)
	customer := createContact(t, h, "Cash summary customer "+t.Name())

	inv := postInvoiceWithLines(t, h, "ACCREC", customer,
		"2026-02-03T00:00:00Z", "2026-03-03T00:00:00Z",
		invoiceLineOn("200", "100"), invoiceLineOn("260", "50"))
	postPaymentForInvoice(t, h, "ACCREC", inv, "090", "150", "2026-02-10")

	cash := cashSummaryReport(t, h)

	// The invoice's coding, not the control account it was settled through.
	assertMoney(t, "sales attributed from the invoice", accountRowValue(t, cash, "200", 1), "100.00")
	assertMoney(t, "the invoice's other revenue line", accountRowValue(t, cash, "260", 1), "50.00")
	assert.NotContains(t, rowLabels(cash), "Accounts Receivable (610)",
		"the receipt must be reported through the control account, not on it")
	assert.Equal(t, []string{"Sales (200)", "Other Revenue (260)"}, labelsOfRows(sectionRows(t, cash, "Income")))
	assert.Empty(t, labelsOfRows(sectionRows(t, cash, "Plus Other Cash Movements")))

	assertMoney(t, "total income", valueByLabel(t, cash, "Total Income"), "150.00")
	assertMoney(t, "total expenses", valueByLabel(t, cash, "Total Expenses"), "0.00")
	assertMoney(t, "net cash movement", valueByLabel(t, cash, "Net Cash Movement"), "150.00")
	assertEqMoney(t, "the movement is the bank accounts' own movement",
		valueByLabel(t, cash, "Net Cash Movement"), cashSummaryLedgerMovement(t, h))
}

// TestHTTP_Reports_CashSummaryLooksThroughPayablesToTheBill is the payable side
// of the same rule: a supplier payment is the bill's coding, not Accounts
// Payable's movement. The bill's line is on an expense account, so the payment
// lands in Less Expenses rather than in Plus Other Cash Movements.
func TestHTTP_Reports_CashSummaryLooksThroughPayablesToTheBill(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)
	supplier := createContact(t, h, "Cash summary supplier "+t.Name())

	bill := postInvoiceWithLines(t, h, "ACCPAY", supplier,
		"2026-02-04T00:00:00Z", "2026-03-04T00:00:00Z", invoiceLineOn("400", "80"))
	postPaymentForInvoice(t, h, "ACCPAY", bill, "090", "80", "2026-02-12")

	cash := cashSummaryReport(t, h)

	assertMoney(t, "advertising attributed from the bill", accountRowValue(t, cash, "400", 1), "80.00")
	assert.NotContains(t, rowLabels(cash), "Accounts Payable (800)",
		"the payment must be reported through the control account, not on it")
	assert.Equal(t, []string{"Advertising (400)"}, labelsOfRows(sectionRows(t, cash, "Less Expenses")))
	assertMoney(t, "total expenses", valueByLabel(t, cash, "Total Expenses"), "80.00")
	assertMoney(t, "net cash movement", valueByLabel(t, cash, "Net Cash Movement"), "-80.00")
	assertEqMoney(t, "the movement is the bank accounts' own movement",
		valueByLabel(t, cash, "Net Cash Movement"), cashSummaryLedgerMovement(t, h))
}

// TestHTTP_Reports_CashSummaryStatesWhatItCannotAttribute is the regression
// test for the tempting alternative: when the ledger links no document to a
// movement of the control account, the money is *not* spread over the accounts
// a document might have coded it to. It stays on the account the journal itself
// named, the row says so, and the report states the total it could not
// attribute — so a reader can see the coverage rather than trust it.
func TestHTTP_Reports_CashSummaryStatesWhatItCannotAttribute(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)

	// A receipt banked straight to Accounts Receivable with no invoice behind
	// it, which is the shape a missing document link takes.
	postManualJournal(t, h, "2026-02-06T00:00:00Z", "cash summary unlinked",
		journalLine("090", "120"), journalLine("610", "-120"))

	cash := cashSummaryReport(t, h)

	unattributed := accountRow(t, cash, "610")
	assert.Equal(t, "Accounts Receivable (610) "+unattributedMark, cell(unattributed, 0),
		"a movement the ledger links no document to must say so in its own row")
	assertMoney(t, "the unattributed movement", dec(cell(unattributed, 1)), "120.00")
	assert.Equal(t, []string{"Accounts Receivable (610) " + unattributedMark},
		labelsOfRows(sectionRows(t, cash, "Plus Other Cash Movements")))

	titles := strings.Join(reportTitles(t, h, "/api/v1/reports/cash-summary?"+cashSummaryWindow), " ")
	assert.Contains(t, titles, "Coverage:", "the report must state its coverage")
	assert.Contains(t, titles, "120.00", "the coverage statement must carry the unattributed amount")

	// The unattributed amount is still in the report, so the report still foots.
	assertMoney(t, "net cash movement", valueByLabel(t, cash, "Net Cash Movement"), "120.00")
	assertEqMoney(t, "net cash movement == the printed rows",
		valueByLabel(t, cash, "Net Cash Movement"), cashSummaryNetMovementAsPrinted(t, cash))
	assertEqMoney(t, "cash balance == opening + net movement",
		valueByLabel(t, cash, "Cash Balance"),
		add(valueByLabel(t, cash, "Opening Balance"), valueByLabel(t, cash, "Net Cash Movement")))
	assertEqMoney(t, "the movement is the bank accounts' own movement",
		valueByLabel(t, cash, "Net Cash Movement"), cashSummaryLedgerMovement(t, h))
}

// TestHTTP_Reports_CashSummaryMeasuresTaxWithoutCountingItTwice pins §1 step 6.
// A ledger outside this one may post its tax to a tax account; this one records
// the tax beside the net amount on the coded line *and* posts it again on 820
// Sales Tax in the same journal. The tax belongs to Plus Tax Movements once:
// the 820 line is the same tax and is not counted again, and it is not a line
// of Plus Other Cash Movements either.
func TestHTTP_Reports_CashSummaryMeasuresTaxWithoutCountingItTwice(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)

	postManualJournal(t, h, "2026-02-07T00:00:00Z", "cash summary taxed spend",
		taxedJournalLine("453", "50", "INPUT", "5.00"),
		journalLine("820", "5.00"),
		journalLine("090", "-55.00"))

	cash := cashSummaryReport(t, h)

	// The spend is the net amount; the tax is the section of its own.
	assertMoney(t, "office expenses net of tax", accountRowValue(t, cash, "453", 1), "50.00")
	assert.NotContains(t, rowLabels(cash), "Sales Tax (820)",
		"the tax account is where the tax was posted, not where it was spent")
	assert.Equal(t, []string{"Office Expenses (453)"}, labelsOfRows(sectionRows(t, cash, "Less Expenses")))
	assert.Empty(t, labelsOfRows(sectionRows(t, cash, "Plus Other Cash Movements")),
		"the tax account and the coded line between them account for the whole spend")

	assertMoney(t, "tax collected", valueByLabel(t, cash, "Tax Collected"), "0.00")
	assertMoney(t, "tax paid is the tax once, not twice", valueByLabel(t, cash, "Tax Paid"), "-5.00")
	assertMoney(t, "net tax movements", valueByLabel(t, cash, "Net Tax Movements"), "-5.00")
	assertMoney(t, "net cash movement", valueByLabel(t, cash, "Net Cash Movement"), "-55.00")
	assertEqMoney(t, "net cash movement == the printed rows",
		valueByLabel(t, cash, "Net Cash Movement"), cashSummaryNetMovementAsPrinted(t, cash))
	assertEqMoney(t, "the movement is the bank accounts' own movement",
		valueByLabel(t, cash, "Net Cash Movement"), cashSummaryLedgerMovement(t, h))
}

// TestHTTP_Reports_CashSummaryFootsAgainstTheBankAccounts puts every shape the
// rule has to handle in one period — a customer receipt, a supplier payment, a
// direct spend carrying tax, and a movement of the control account with no
// document behind it — and asserts the property that makes the report
// checkable rather than merely self-consistent: the rows it prints add up to
// the bank accounts' own movement, and the closing balance is the opening
// balance moved by them.
func TestHTTP_Reports_CashSummaryFootsAgainstTheBankAccounts(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)
	customer := createContact(t, h, "Cash summary footing customer "+t.Name())
	supplier := createContact(t, h, "Cash summary footing supplier "+t.Name())

	inv := postInvoiceWithLines(t, h, "ACCREC", customer,
		"2026-02-03T00:00:00Z", "2026-03-03T00:00:00Z", invoiceLineOn("200", "100"))
	postPaymentForInvoice(t, h, "ACCREC", inv, "090", "100", "2026-02-10")
	bill := postInvoiceWithLines(t, h, "ACCPAY", supplier,
		"2026-02-04T00:00:00Z", "2026-03-04T00:00:00Z", invoiceLineOn("400", "40"))
	postPaymentForInvoice(t, h, "ACCPAY", bill, "090", "40", "2026-02-12")
	postManualJournal(t, h, "2026-02-18T00:00:00Z", "cash summary tax spend",
		taxedJournalLine("453", "50", "INPUT", "5.00"),
		journalLine("820", "5.00"),
		journalLine("090", "-55.00"))
	postManualJournal(t, h, "2026-02-20T00:00:00Z", "cash summary unlinked receipt",
		journalLine("090", "30"), journalLine("610", "-30"))

	cash := cashSummaryReport(t, h)

	// 100 in, 40 and 50 out, 5 of tax on the 50, and 30 in that no document
	// explains: the rows come to 35 and so must the bank accounts.
	assertMoney(t, "net cash movement", valueByLabel(t, cash, "Net Cash Movement"), "35.00")
	assertEqMoney(t, "net cash movement == the printed rows",
		valueByLabel(t, cash, "Net Cash Movement"), cashSummaryNetMovementAsPrinted(t, cash))
	assertEqMoney(t, "net cash movement == the bank accounts' own movement",
		valueByLabel(t, cash, "Net Cash Movement"), cashSummaryLedgerMovement(t, h))
	assertEqMoney(t, "cash balance == opening + net movement",
		valueByLabel(t, cash, "Cash Balance"),
		add(valueByLabel(t, cash, "Opening Balance"), valueByLabel(t, cash, "Net Cash Movement")))

	// Nothing is missing from the sections: every posted account is in the one
	// its own role puts it in, and the control accounts are gone.
	assertMoney(t, "income", valueByLabel(t, cash, "Total Income"), "100.00")
	assertMoney(t, "expenses", valueByLabel(t, cash, "Total Expenses"), "90.00")
	assertMoney(t, "other cash movements", valueByLabel(t, cash, "Total Other Cash Movements"), "30.00")
	assertMoney(t, "net tax movements", valueByLabel(t, cash, "Net Tax Movements"), "-5.00")
	titles := strings.Join(reportTitles(t, h, "/api/v1/reports/cash-summary?"+cashSummaryWindow), " ")
	assert.Contains(t, titles, "Coverage: 30.00", "the coverage statement must carry the unattributed amount")
}

// invoiceLineOn is a no-tax invoice line coded to an account of the test's
// choosing.
func invoiceLineOn(code, amount string) map[string]any {
	return map[string]any{
		"Description": "cash summary", "Quantity": "1", "UnitAmount": amount, "AccountCode": code,
	}
}

// labelsOfRows lists the first cell of the account rows of a section.
func labelsOfRows(rows []xeroReportRow) []string {
	var out []string
	for _, row := range rows {
		if row.RowType != "Row" {
			continue
		}
		out = append(out, cell(row, 0))
	}
	return out
}

// cashSummaryLedgerMovement reads the movement of the organisation's bank
// accounts straight out of the ledger, so the report's own figure is checked
// against the books rather than against a number typed into the test.
func cashSummaryLedgerMovement(t *testing.T, h *appHarness) decimalLike {
	t.Helper()
	var movement decimalLike
	err := h.repos.Pool.QueryRow(context.Background(), `
		SELECT COALESCE(SUM(l.net_amount), 0)
		  FROM gl_journal_lines l
		  JOIN gl_journals j ON j.journal_id = l.journal_id
		  JOIN accounts a ON a.account_id = l.account_id
		 WHERE j.organisation_id = $1 AND a.type = 'BANK'
		   AND j.journal_date BETWEEN DATE '2026-02-01' AND DATE '2026-02-28'`,
		seedDemoOrgID).Scan(&movement.f)
	require.NoError(t, err)
	return movement
}
