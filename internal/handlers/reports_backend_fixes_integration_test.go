package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
)

// This file is the regression suite for the four report endpoints that used to
// answer 200 with something other than the report they are named after:
//
//	/reports/trial-balance        measured one period and dropped a whole class
//	/reports/cash-summary         returned the Bank Summary payload
//	/reports/aged-*-by-contact    returned the aged summary, unrelabelled
//	/reports/bas, /reports/sales-tax
//	                              returned a header with no rows and no total
//
// Each test below asserts the shape the report is supposed to have and the
// arithmetic that ties it to the ledger, so a regression fails here rather than
// in a reader's hands.

// fixFixture is a small, fully controlled ledger. It is deliberately spread
// across two financial years: the prior year's postings are what a report that
// only looks at its own window drops on the floor.
type fixFixture struct {
	h        *appHarness
	customer string
	supplier string
}

func seedFixFixture(t *testing.T) fixFixture {
	t.Helper()
	h := newHarness(t)
	resetLedger(t, h)
	f := fixFixture{
		h:        h,
		customer: createContact(t, h, "Fix customer "+uuid.NewString()[:6]),
		supplier: createContact(t, h, "Fix supplier "+uuid.NewString()[:6]),
	}

	// Prior financial year — outside every window queried below.
	postManualJournal(t, h, "2025-06-15T00:00:00Z", "py-bank",
		journalLine("090", "500"), journalLine("800", "-500")) // bank balance 500
	postManualJournal(t, h, "2025-06-20T00:00:00Z", "py-rent",
		journalLine("469", "40"), journalLine("090", "-40")) // rent expense 40

	// Reported year, February 2026.
	postManualJournal(t, h, "2026-02-10T00:00:00Z", "fy-rent",
		journalLine("469", "100"), journalLine("800", "-100")) // rent expense 100
	postManualJournal(t, h, "2026-02-20T00:00:00Z", "fy-sale",
		journalLine("610", "250"), journalLine("200", "-250")) // sales income 250
	postManualJournal(t, h, "2026-02-25T00:00:00Z", "fy-bank-in",
		journalLine("090", "300"), journalLine("200", "-300")) // cash received 300
	postManualJournal(t, h, "2026-02-26T00:00:00Z", "fy-bank-out",
		journalLine("469", "50"), journalLine("090", "-50")) // cash spent 50

	// Tax-bearing postings: the tax amount rides on the coded line, which is
	// where this ledger records it. Each rate is posted on its own date so a
	// window can be opened around one of them.
	postManualJournal(t, h, "2026-02-05T00:00:00Z", "tax-sale",
		taxedJournalLine("200", "-100", "OUTPUT", "8.25"), journalLine("610", "100"))
	postManualJournal(t, h, "2026-02-05T00:00:00Z", "tax-purchase",
		taxedJournalLine("400", "50", "OUTPUT", "4.12"), journalLine("800", "-50"))
	postManualJournal(t, h, "2026-02-15T00:00:00Z", "tax-sale-only",
		taxedJournalLine("200", "-100", "OUTPUT2", "8.75"), journalLine("610", "100"))
	postManualJournal(t, h, "2026-02-18T00:00:00Z", "tax-purchase-only",
		taxedJournalLine("400", "50", "INPUT", "4.12"), journalLine("800", "-50"))

	// Receivables and payables for the by-contact report.
	postInvoice(t, h, "ACCREC", f.customer, "2026-01-05T00:00:00Z", "2026-02-05T00:00:00Z", "10")
	postInvoice(t, h, "ACCREC", f.customer, "2026-02-20T00:00:00Z", "2026-03-20T00:00:00Z", "20")
	postInvoice(t, h, "ACCPAY", f.supplier, "2026-02-01T00:00:00Z", "2026-03-01T00:00:00Z", "70")
	return f
}

// taxedJournalLine is a manual-journal line that carries a tax rate and the tax
// amount the ledger recorded for it.
func taxedJournalLine(code, amount, taxType, taxAmount string) map[string]any {
	return map[string]any{
		"Description": "fix", "AccountCode": code, "LineAmount": amount,
		"TaxType": taxType, "TaxAmount": taxAmount,
	}
}

// closingBalances reads the organisation's own closing balances out of the
// ledger, so the assertions below are checked against the books rather than
// against a number typed into the test.
func closingBalances(t *testing.T, h *appHarness, asOf string) map[string]string {
	t.Helper()
	rows, err := h.repos.Pool.Query(context.Background(), `
		SELECT a.code, COALESCE(SUM(l.net_amount), 0)
		  FROM accounts a
		  LEFT JOIN gl_journal_lines l ON l.account_id = a.account_id
		  LEFT JOIN gl_journals j ON j.journal_id = l.journal_id
		 WHERE a.organisation_id = $1
		   AND (l.journal_id IS NULL OR j.journal_date <= $2::date)
		 GROUP BY a.code
		HAVING COALESCE(SUM(l.net_amount), 0) <> 0`, seedDemoOrgID, asOf)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var code string
		var amount string
		require.NoError(t, rows.Scan(&code, &amount))
		out[code] = amount
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, out, "the fixture posted nothing")
	return out
}

// ---------- defect 1: trial balance ------------------------------------------

// TestHTTP_Reports_TrialBalanceRendersEveryAccountWithABalance is the regression
// test for a trial balance that measured a single period: an account whose
// journals all fell outside the window was dropped from the report entirely, so
// whole account types could vanish from the trial balance and from its total.
// It also pins the two measures Xero uses in one table — the balance carried as
// at the report date for balance sheet accounts, the year-to-date movement for
// profit and loss accounts — and the property that makes the total checkable:
// it is the sum of the rows rendered above it.
func TestHTTP_Reports_TrialBalanceRendersEveryAccountWithABalance(t *testing.T) {
	f := seedFixFixture(t)
	const asOf = "2026-03-31"

	tb := fetchReport(t, f.h, "/api/v1/reports/trial-balance?date="+asOf)

	// Every account carrying a balance at the report date appears, whatever
	// period its journals fall in — including the one whose only postings are
	// in the prior financial year.
	for code := range closingBalances(t, f.h, asOf) {
		requireHasAccountRow(t, tb, code)
	}
	requireHasAccountRow(t, tb, "090") // the prior-year balance sheet movement

	// Balance sheet: the balance carried as at the report date, even though
	// nothing moved in the reported period or in the financial year to date.
	// The expected figure is the account's own closing balance in the ledger.
	balances := closingBalances(t, f.h, asOf)
	bank := accountRow(t, tb, "090")
	assertMoney(t, "trial balance 090 period debit", dec(cell(bank, 1)), "0.00")
	assertMoney(t, "trial balance 090 period credit", dec(cell(bank, 2)), "0.00")
	assertEqMoney(t, "trial balance 090 YTD debit == the ledger's closing balance",
		dec(cell(bank, 3)), dec(balances["090"]))

	payable := accountRow(t, tb, "800")
	assertEqMoney(t, "trial balance 800 YTD credit == the ledger's closing balance",
		dec(cell(payable, 4)), dec(balances["800"]).Abs())

	// Profit and loss: the year-to-date movement, not the balance it carries.
	// Rent was posted 40.00 in the prior year and 150.00 in this one, so a
	// report that used the balance would print 190.00 here.
	rent := accountRow(t, tb, "469")
	assertMoney(t, "trial balance 469 YTD debit", dec(cell(rent, 3)), "150.00")

	// The total is the sum of the rows rendered above it, column by column, so
	// it can be checked against the report rather than recomputed from a query
	// the report does not run.
	assertTrialBalanceTotalIsTheSumOfItsRows(t, tb)
}

// requireHasAccountRow fails unless the trial balance renders the account.
func requireHasAccountRow(t *testing.T, env xeroReportEnvelope, code string) {
	t.Helper()
	assert.NotEmptyf(t, accountRowOrEmpty(env, code), "trial balance has no row for account %s", code)
}

// accountRow returns the trial balance row for an account code.
func accountRow(t *testing.T, env xeroReportEnvelope, code string) xeroReportRow {
	t.Helper()
	row := accountRowOrEmpty(env, code)
	require.NotEmptyf(t, row.Cells, "trial balance has no row for account %s", code)
	return row
}

func accountRowOrEmpty(env xeroReportEnvelope, code string) xeroReportRow {
	for _, row := range allRows(env) {
		if row.RowType == "Row" && len(row.Cells) == 5 && strings.Contains(cell(row, 0), "("+code+")") {
			return row
		}
	}
	return xeroReportRow{}
}

// ---------- defect 2: cash summary -------------------------------------------

// TestHTTP_Reports_CashSummaryIsItsOwnReport is the regression test for the
// cash summary that answered with the Bank Summary payload under a different
// name: the two reports had different titles and identical bodies, so a client
// asking where the cash went was told where it ended up.
//
// It pins the shape of Xero's Cash Summary — income, less expenses, other cash
// movements and tax movements, closed by the balances either side of the window
// — and the property that makes the report checkable rather than merely
// self-consistent: Net Cash Movement is the sum of the rows above it and, at
// the same time, the bank accounts' own movement over the window.
func TestHTTP_Reports_CashSummaryIsItsOwnReport(t *testing.T) {
	f := seedFixFixture(t)
	const window = "fromDate=2026-02-01&toDate=2026-02-28"

	cash := fetchReport(t, f.h, "/api/v1/reports/cash-summary?"+window)
	bank := fetchReport(t, f.h, "/api/v1/reports/bank-summary?"+window)

	assert.Equal(t, "CashSummary", cash.Reports[0].ReportID)
	assert.Equal(t, "Cash Summary", cash.Reports[0].ReportName)
	assert.Equal(t, "BankSummary", bank.Reports[0].ReportID)

	// The two reports are not one body under two names: the Bank Summary is each
	// account's movement and balance, the Cash Summary says where the money
	// moved to and from.
	assert.NotEqual(t, rowLabels(cash), rowLabels(bank), "the two reports must not share a body")
	assert.NotEqual(t, headerLabels(cash), headerLabels(bank), "the two reports must not share their columns")

	// Xero's Cash Summary grid: the period's own year, then the comparative
	// columns. The report is rendered for a single period and the organisation
	// stores no budget, so those three columns are present and empty — never a
	// 0.00, which would read as a measurement the organisation has not taken.
	assert.Equal(t,
		[]string{"", "2026", "Yearly average (YTD)", "Variance", "Variance for Variance"},
		headerLabels(cash))
	for _, row := range allRows(cash) {
		if row.RowType == "Section" || row.RowType == "Header" {
			continue
		}
		require.Len(t, row.Cells, 5, "row "+cell(row, 0)+" must carry the period and the comparative columns")
		for col := 2; col <= 4; col++ {
			assert.Equalf(t, "", cell(row, col),
				"row %q column %d must be empty, not a printed zero", cell(row, 0), col)
		}
	}

	// The reference layout, in the reference's own order, with the lines this
	// ledger can fill and the ones it cannot.
	assert.Equal(t, []string{
		"Income", "Sales (200)", "Total Income", "Less Expenses", "Rent (469)", "Total Expenses",
		"Surplus (Deficit)", "Plus Other Cash Movements", "Total Other Cash Movements",
		"Plus Tax Movements", "Tax Collected", "Tax Paid", "Net Tax Movements", "Net Cash Movement",
		"Summary", "Opening Balance", "Plus Net Cash Movement", "Cash Balance",
	}, rowLabels(cash))
	assert.NotContains(t, rowLabels(cash), "February 2026",
		"the movement is attributed to the accounts it moved to and from, not listed as months")

	// The tax section is measured rather than merely present: every attributed
	// line hands its own tax here and keeps its net, so the three lines carry
	// figures even when the period's postings happen to carry no tax at all.
	// The titles say what the section measures and how much of the movement
	// could not be attributed, rather than naming a part of the layout the
	// ledger cannot reach.
	tax := findRowByLabel(t, cash, "Plus Tax Movements")
	for _, label := range []string{"Tax Collected", "Tax Paid", "Net Tax Movements"} {
		row := rowByLabel(t, tax.Rows, label)
		require.NotEmptyf(t, row.Cells, "the tax section has no %q line", label)
		assert.Equalf(t, "0.00", cell(row, 1), "%s must be a measured figure", label)
	}
	titles := strings.Join(reportTitles(t, f.h, "/api/v1/reports/cash-summary?"+window), " ")
	assert.Contains(t, titles, "Tax Movements", "the titles must say what the tax section measures")
	assert.Contains(t, titles, "Coverage:", "the titles must state how much of the movement is unattributed")

	// The window's cash: 300 received against sales, 50 spent on rent, and the
	// expenses are shown as amounts spent rather than as negative income.
	assertMoney(t, "cash summary total income", valueByLabel(t, cash, "Total Income"), "300.00")
	assertMoney(t, "cash summary total expenses", valueByLabel(t, cash, "Total Expenses"), "50.00")
	assertMoney(t, "cash summary surplus", valueByLabel(t, cash, "Surplus (Deficit)"), "250.00")
	assertMoney(t, "cash summary net movement", valueByLabel(t, cash, "Net Cash Movement"), "250.00")
	assertMoney(t, "cash summary opening balance", valueByLabel(t, cash, "Opening Balance"), "460.00")
	assertMoney(t, "cash summary cash balance", valueByLabel(t, cash, "Cash Balance"), "710.00")

	// Each figure is the sum of the rows above it, so the report is checkable
	// against itself...
	assertEqMoney(t, "surplus == total income less total expenses",
		valueByLabel(t, cash, "Surplus (Deficit)"),
		sub(valueByLabel(t, cash, "Total Income"), valueByLabel(t, cash, "Total Expenses")))
	assertEqMoney(t, "net cash movement == surplus + total other cash movements + net tax movements",
		valueByLabel(t, cash, "Net Cash Movement"),
		add(add(valueByLabel(t, cash, "Surplus (Deficit)"), valueByLabel(t, cash, "Total Other Cash Movements")),
			valueByLabel(t, cash, "Net Tax Movements")))
	assertEqMoney(t, "opening + net movement == cash balance",
		valueByLabel(t, cash, "Cash Balance"),
		add(valueByLabel(t, cash, "Opening Balance"), valueByLabel(t, cash, "Net Cash Movement")))

	// ...and against the ledger and the other report, which measure it in their
	// own ways: the movement is the bank accounts' change over the window.
	before := closingBalances(t, f.h, "2026-01-31")
	after := closingBalances(t, f.h, "2026-02-28")
	var ledgerMovement decimalLike
	for _, code := range bankAccountCodes(t, f.h) {
		ledgerMovement = add(ledgerMovement, sub(dec(after[code]), dec(before[code])))
	}
	assertEqMoney(t, "net cash movement == the bank accounts' movement in the ledger",
		valueByLabel(t, cash, "Net Cash Movement"), ledgerMovement)

	bankTotal := rowByLabel(t, allRows(bank), "Total")
	require.NotEmpty(t, bankTotal.Cells, "the bank summary has no total row")
	assertEqMoney(t, "net cash movement == Bank Summary closing less opening",
		valueByLabel(t, cash, "Net Cash Movement"),
		sub(dec(cell(bankTotal, 4)), dec(cell(bankTotal, 1))))

	// The defect itself: the cash summary must not be the Bank Summary's
	// per-account view. No bank account is rendered as a row of its own.
	bankRows := map[string]bool{}
	for _, label := range rowLabels(bank) {
		bankRows[label] = true
	}
	for _, label := range rowLabels(cash) {
		assert.Falsef(t, bankRows[label], "the cash summary rendered the bank account row %q", label)
	}
}

// reportTitles reads the report's title block straight off the wire: the
// envelope these tests share does not carry it, and the titles are where a
// section the stored data cannot measure is named.
func reportTitles(t *testing.T, h *appHarness, path string) []string {
	t.Helper()
	status, body := h.do(t, http.MethodGet, path, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var env struct {
		Reports []struct {
			ReportTitles []string `json:"ReportTitles"`
		} `json:"Reports"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.NotEmpty(t, env.Reports, "the envelope carried no report")
	return env.Reports[0].ReportTitles
}

// bankAccountCodes reads the organisation's bank account codes out of its own
// chart of accounts, so the movement the report is checked against is taken
// from the books rather than named in the test.
func bankAccountCodes(t *testing.T, h *appHarness) []string {
	t.Helper()
	rows, err := h.repos.Pool.Query(context.Background(),
		`SELECT code FROM accounts WHERE organisation_id = $1 AND type = $2 ORDER BY code`,
		seedDemoOrgID, models.AccountTypeBank)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var code string
		require.NoError(t, rows.Scan(&code))
		out = append(out, code)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, out, "the organisation has no bank account to check the movement against")
	return out
}

// rowByLabel finds a row by its first cell inside one set of rows.
func rowByLabel(t *testing.T, rows []xeroReportRow, label string) xeroReportRow {
	t.Helper()
	for _, row := range rows {
		if len(row.Cells) > 0 && row.Cells[0].Value == label {
			return row
		}
	}
	return xeroReportRow{}
}

// headerLabels lists the column headings of the report's table.
func headerLabels(env xeroReportEnvelope) []string {
	for _, row := range allRows(env) {
		if row.RowType == "Header" {
			var out []string
			for _, c := range row.Cells {
				out = append(out, c.Value)
			}
			return out
		}
	}
	return nil
}

// rowLabels lists the first cell of every row in the report, sections included.
func rowLabels(env xeroReportEnvelope) []string {
	var out []string
	for _, row := range allRows(env) {
		if len(row.Cells) == 0 {
			continue
		}
		label := row.Cells[0].Value
		if row.Title != "" && label == "" {
			label = row.Title
		}
		if label != "" {
			out = append(out, label)
		}
	}
	return out
}

// ---------- defect 3: aged by contact ----------------------------------------

// TestHTTP_Reports_AgedByContactIsTheContactDrillDown is the regression test for
// the two by-contact endpoints, which returned the aged *summary* report with
// the summary report's own name: a client asking for the invoices behind a
// contact's balance was handed the balance again, under a title that said
// otherwise.
func TestHTTP_Reports_AgedByContactIsTheContactDrillDown(t *testing.T) {
	f := seedFixFixture(t)
	const asOf = "2026-03-31"

	byContact := fetchReport(t, f.h, "/api/v1/reports/aged-receivables-by-contact?date="+asOf)
	summary := fetchReport(t, f.h, "/api/v1/reports/aged-receivables?date="+asOf)

	assert.Equal(t, "AgedReceivablesByContact", byContact.Reports[0].ReportID)
	assert.Equal(t, "Aged Receivables by Contact", byContact.Reports[0].ReportName)
	assert.NotEqual(t, summary.Reports[0].ReportID, byContact.Reports[0].ReportID,
		"the by-contact report must not answer as the summary report")

	// One section per contact, holding that contact's invoices and its own
	// subtotal. The summary report has no such rows.
	var invoiceRows, contactSections int
	for _, row := range byContact.Reports[0].Rows {
		if row.RowType != "Section" {
			continue
		}
		contactSections++
		var rows int
		var subtotal decimalLike
		for _, r := range row.Rows {
			if r.RowType != "Row" || len(r.Cells) != 7 {
				continue
			}
			rows++
			subtotal = add(subtotal, dec(cell(r, 6)))
		}
		invoiceRows += rows
		require.NotEqualf(t, 0, rows, "contact section %q holds no invoices", row.Title)
		total := rowByLabel(t, row.Rows, "Total "+row.Title)
		require.NotEmptyf(t, total.Cells, "contact section %q has no subtotal", row.Title)
		assertEqMoney(t, "contact subtotal for "+row.Title, subtotal, dec(cell(total, 6)))
	}
	assert.Equal(t, 1, contactSections, "the fixture has one customer with receivables")
	assert.Equal(t, 2, invoiceRows, "the customer has two invoices outstanding")

	// It is the same money as the summary report, drilled down.
	assertEqMoney(t, "by-contact grand total == aged receivables total",
		agedTotal(t, byContact), agedTotal(t, summary))

	// ?contactID narrows the drill-down to one contact.
	mine := fetchReport(t, f.h, "/api/v1/reports/aged-receivables-by-contact?date="+asOf+"&contactID="+f.customer)
	assertEqMoney(t, "by-contact total for the customer", agedTotal(t, mine), agedTotal(t, summary))
	other := fetchReport(t, f.h, "/api/v1/reports/aged-receivables-by-contact?date="+asOf+"&contactID="+f.supplier)
	assertEqMoney(t, "by-contact total for the supplier", agedTotal(t, other), dec(""))
}

// ---------- defect 4: BAS / sales tax ----------------------------------------

// TestHTTP_Reports_SalesTaxRowsFollowTheLedger is the regression test for the
// BAS and Sales Tax reports, which returned a header row and nothing else — no
// rate, no amount, no total. The rows must be the rates the organisation's own
// coded journals carry (not a fixed list), the totals must add up to the rows,
// and a tax column the ledger cannot support must be left empty rather than
// filled with a zero that reads like a measurement.
func TestHTTP_Reports_SalesTaxRowsFollowTheLedger(t *testing.T) {
	f := seedFixFixture(t)

	// Only the rate posted on 2026-02-05 has both a sale and a purchase, so it
	// is the one window where both tax columns are measurable.
	both := fetchReport(t, f.h, "/api/v1/reports/sales-tax?fromDate=2026-02-04&toDate=2026-02-06")
	rows := amountRows(both)
	require.Len(t, rows, 1, "one rate was posted in this window")
	assert.Equal(t, "Tax on Consulting (8.25%)", taxRowLabel(t, f.h, "OUTPUT"),
		"the row is named from the organisation's own tax rates")
	assert.Equal(t, taxRowLabel(t, f.h, "OUTPUT"), cell(rows[0], 0))
	assertMoney(t, "net sales under the rate", dec(cell(rows[0], 1)), "100.00")
	assertMoney(t, "net purchases under the rate", dec(cell(rows[0], 2)), "50.00")
	assertMoney(t, "tax collected under the rate", dec(cell(rows[0], 3)), "8.25")
	assertMoney(t, "tax paid under the rate", dec(cell(rows[0], 4)), "4.12")
	assertMoney(t, "net tax under the rate", dec(cell(rows[0], 5)), "4.13")

	// With every rate measured, the Total row carries the tax columns too, and
	// each of its cells is the sum of the rows above it.
	total := findRowByLabel(t, both, "Total")
	for col := 1; col <= 5; col++ {
		var summed decimalLike
		for _, row := range rows {
			summed = add(summed, dec(cell(row, col)))
		}
		assertEqMoney(t, "sales tax total column "+strconv.Itoa(col), dec(cell(total, col)), summed)
	}

	// The whole month carries three rates, and two of them posted to one side
	// only. A rate that never posted to a side has nothing to measure there and
	// does not hold that side's total back, so the workpaper is complete and the
	// Total row carries both tax columns and Net Tax.
	month := fetchReport(t, f.h, "/api/v1/reports/sales-tax?fromDate=2026-02-01&toDate=2026-02-28")
	monthRows := amountRows(month)
	require.Len(t, monthRows, 3, "three rates were posted this month")
	monthTotal := findRowByLabel(t, month, "Total")
	assert.Equal(t, "Total", cell(monthTotal, 0),
		"the ledger records a tax for every tax-typed line here, so the total needs no qualifier")
	assertMoney(t, "monthly tax collected", dec(cell(monthTotal, 3)), "17.00")
	assertMoney(t, "monthly tax paid", dec(cell(monthTotal, 4)), "8.24")
	assertMoney(t, "monthly net tax", dec(cell(monthTotal, 5)), "8.76")

	// Each rate's own Net Tax is printed even where the rate posted to one side
	// only — that side's tax is zero because the rate has no lines there — so
	// the column of nets adds up to the Net Tax total above it.
	var netSum decimalLike
	for _, row := range monthRows {
		netSum = add(netSum, dec(cell(row, 5)))
	}
	assertEqMoney(t, "the rates' net taxes sum to the total", dec(cell(monthTotal, 5)), netSum)
	assert.Contains(t, strings.Join(rowLabels(month), " "),
		"are the whole of what the ledger holds",
		"the report must state what its tax columns measure")

	// A window whose lines carry no tax at all reports no rate and no invented
	// zero: the columns are empty and the note says why.
	empty := fetchReport(t, f.h, "/api/v1/reports/bas?fromDate=2026-02-19&toDate=2026-02-25")
	require.Empty(t, amountRows(empty), "no line in this window carries a tax type")
	totalRow := findRowByLabel(t, empty, "Total")
	assert.Equal(t, "", cell(totalRow, 3), "an unmeasured tax column must be empty, not 0.00")
	assert.Equal(t, "", cell(totalRow, 4), "an unmeasured tax column must be empty, not 0.00")
	assert.Contains(t, strings.Join(rowLabels(empty), " "), "No tax rate is represented",
		"the report must state what it could not represent")
}

// amountRows returns the rate rows of a sales-tax workpaper (every Row row).
func amountRows(env xeroReportEnvelope) []xeroReportRow {
	var out []xeroReportRow
	for _, row := range allRows(env) {
		if row.RowType == "Row" && len(row.Cells) == 6 && cell(row, 0) != "" {
			out = append(out, row)
		}
	}
	return out
}

// taxRowLabel reads the name the organisation gave a tax type, straight from
// its tax-rate table — the report must not carry a list of rates of its own.
func taxRowLabel(t *testing.T, h *appHarness, taxType string) string {
	t.Helper()
	var name string
	err := h.repos.Pool.QueryRow(context.Background(),
		`SELECT name FROM tax_rates WHERE organisation_id = $1 AND tax_type = $2 ORDER BY name LIMIT 1`,
		seedDemoOrgID, taxType).Scan(&name)
	require.NoErrorf(t, err, "no tax rate named %s", taxType)
	return name
}

// ---------- defect 1, part two: the net basis --------------------------------

// TestHTTP_Reports_TrialBalanceNetsEachAccountBeforeTotalling is the regression
// test for the trial balance that summed gross debits and gross credits into two
// independent columns. A balanced ledger gives equal totals either way — every
// posting is one side or the other — so the report balanced while printing
// 108,392.54 where Xero prints 42,595.46, and an account that moved both ways
// read as the sum of its movement rather than as its net movement. The reference
// ledger's Sales is the live case: Dr 1,019.95 / Cr 30,559.13 is one figure,
// 29,539.18, on one side.
func TestHTTP_Reports_TrialBalanceNetsEachAccountBeforeTotalling(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)

	// One account, both directions, inside one window: a sale of 100 with 30 of
	// it refunded. Its movement is 70 — two-sided columns printed 100 and 30,
	// and the two totals they produced read 130.
	postManualJournal(t, h, "2026-05-04T00:00:00Z", "net-sale",
		journalLine("200", "-100"), journalLine("610", "100"))
	postManualJournal(t, h, "2026-05-18T00:00:00Z", "net-refund",
		journalLine("200", "30"), journalLine("610", "-30"))

	tb := fetchReport(t, h, "/api/v1/reports/trial-balance?fromDate=2026-01-01&toDate=2026-05-31")

	sales := accountRow(t, tb, "200")
	assertMoney(t, "trial balance 200 period debit", dec(cell(sales, 1)), "0.00")
	assertMoney(t, "trial balance 200 period credit", dec(cell(sales, 2)), "70.00")
	assertMoney(t, "trial balance 200 YTD debit", dec(cell(sales, 3)), "0.00")
	assertMoney(t, "trial balance 200 YTD credit", dec(cell(sales, 4)), "70.00")

	// Its counterpart nets the same way, on the other side.
	receivable := accountRow(t, tb, "610")
	assertMoney(t, "trial balance 610 period debit", dec(cell(receivable, 1)), "70.00")
	assertMoney(t, "trial balance 610 period credit", dec(cell(receivable, 2)), "0.00")

	// The totals are the netted movement on each side. Gross had them at 130
	// each: the same figure, and the wrong one.
	total := findRowByLabel(t, tb, "Total")
	assertEqMoney(t, "trial balance Total debit == Total credit",
		dec(cell(total, 1)), dec(cell(total, 2)))
	assertMoney(t, "trial balance Total debit is the netted movement", dec(cell(total, 1)), "70.00")
	assertMoney(t, "trial balance Total YTD debit is the netted movement", dec(cell(total, 3)), "70.00")

	// No account may read two-sided in a pair: that is the shape the defect
	// produced and the shape Xero never prints.
	for _, row := range allRows(tb) {
		if row.RowType != "Row" || len(row.Cells) != 5 {
			continue
		}
		for _, pair := range [][2]int{{1, 2}, {3, 4}} {
			assert.Falsef(t, dec(cell(row, pair[0])).f != 0 && dec(cell(row, pair[1])).f != 0,
				"row %q carries both sides of one pair: %s / %s",
				cell(row, 0), cell(row, pair[0]), cell(row, pair[1]))
		}
	}
}

// ---------- defect 2, part two: the rest of the query string -----------------

// titledReport is the part of the envelope these assertions read: what the
// report says it is and when it says it is drawn at. It is declared here rather
// than added to the shared envelope so the reports that have no titles to check
// keep decoding exactly what they decoded before.
type titledReport struct {
	ReportID     string   `json:"ReportID"`
	ReportDate   string   `json:"ReportDate"`
	ReportTitles []string `json:"ReportTitles"`
}

func fetchTitledReport(t *testing.T, h *appHarness, path string) titledReport {
	t.Helper()
	status, body := h.do(t, http.MethodGet, path, nil, true)
	require.Equalf(t, http.StatusOK, status, "%s: %s", path, string(body))
	var env struct {
		Reports []titledReport `json:"Reports"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.NotEmptyf(t, env.Reports, "%s returned no report", path)
	return env.Reports[0]
}

// TestHTTP_Reports_ToDateSetsThePeriod is the regression test for the three
// endpoints that read `date` and ignored the rest of their query string: a
// caller who asked for a period was handed a report dated today, so
// ?toDate=2026-12-31 on the Executive Summary printed "For the period ending 11
// September 2026" and the same window on the Budget Summary printed "For the
// year to 11 September 2026". Each endpoint below is probed three ways — toDate
// alone, fromDate with toDate, and a `date` sent alongside a contradictory
// toDate — and then once with no parameters at all, which must still mean today.
func TestHTTP_Reports_ToDateSetsThePeriod(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)

	// February alone. A report dated at today covers none of it.
	postManualJournal(t, h, "2026-02-10T00:00:00Z", "feb-rent",
		journalLine("469", "120"), journalLine("800", "-120"))
	today := time.Now().UTC().Format("2 January 2006")

	// --- Trial Balance: toDate is the as-at date, so the period follows it ---
	tb := fetchReport(t, h, "/api/v1/reports/trial-balance?toDate=2026-02-28")
	debits, credits := sumTrialBalanceTotals(t, tb)
	assertMoney(t, "trial balance period debits follow toDate", debits, "120.00")
	assertMoney(t, "trial balance period credits follow toDate", credits, "120.00")

	// fromDate with toDate is the period, and the report names it.
	named := fetchTitledReport(t, h, "/api/v1/reports/trial-balance?fromDate=2026-02-01&toDate=2026-02-28")
	assert.Contains(t, strings.Join(named.ReportTitles, " "), "1 February 2026")
	assert.Contains(t, strings.Join(named.ReportTitles, " "), "28 February 2026")

	// `date` stays authoritative when both are sent: the December toDate would
	// put every February posting outside the report.
	tb = fetchReport(t, h, "/api/v1/reports/trial-balance?date=2026-02-28&toDate=2025-12-31")
	debits, _ = sumTrialBalanceTotals(t, tb)
	assertMoney(t, "trial balance reads date, not toDate, when both are sent", debits, "120.00")

	// --- Executive Summary: the period, and the day the balances are as at ---
	exec := fetchTitledReport(t, h, "/api/v1/reports/executive-summary?toDate=2026-02-28")
	assert.Equal(t, "28 February 2026", exec.ReportDate)
	assert.Contains(t, strings.Join(exec.ReportTitles, " "), "For the period ending 28 February 2026")
	env := fetchReport(t, h, "/api/v1/reports/executive-summary?toDate=2026-02-28")
	assertMoney(t, "executive summary expenses cover the requested month",
		valueByLabel(t, env, "Other Expenses"), "120.00")

	win := fetchReport(t, h, "/api/v1/reports/executive-summary?fromDate=2026-01-01&toDate=2026-02-28")
	assert.Equal(t,
		"1 January 2026 - 28 February 2026", cell(allRows(win)[0], 1),
		"the KPI column must be headed by the period asked for")

	exec = fetchTitledReport(t, h, "/api/v1/reports/executive-summary?date=2026-02-28&toDate=2025-12-31")
	assert.Equal(t, "28 February 2026", exec.ReportDate,
		"date wins over toDate on the executive summary too")

	// --- Budget Summary: it has no rows to date, so the window is its title ---
	budget := fetchTitledReport(t, h, "/api/v1/reports/budget-summary?toDate=2026-02-28")
	assert.Equal(t, "28 February 2026", budget.ReportDate)
	assert.Equal(t, "For the year to 28 February 2026", budget.ReportTitles[2])

	budget = fetchTitledReport(t, h, "/api/v1/reports/budget-summary?fromDate=2026-01-01&toDate=2026-02-28")
	assert.Equal(t, "From 1 January 2026 To 28 February 2026", budget.ReportTitles[2],
		"a caller who asks for a window must be told which window they got")

	budget = fetchTitledReport(t, h, "/api/v1/reports/budget-summary?date=2026-02-28&toDate=2025-12-31")
	assert.Equal(t, "28 February 2026", budget.ReportDate,
		"date wins over toDate on the budget summary too")

	// --- No parameter at all still means today, on all three ---
	for _, path := range []string{
		"/api/v1/reports/trial-balance",
		"/api/v1/reports/executive-summary",
		"/api/v1/reports/budget-summary",
	} {
		assert.Equalf(t, today, fetchTitledReport(t, h, path).ReportDate,
			"%s: the default date must stay today", path)
	}
}
