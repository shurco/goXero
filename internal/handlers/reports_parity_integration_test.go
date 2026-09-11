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
)

// This file holds the regression suite for the report backend. Each test seeds
// a deterministic set of GL postings through the public API (manual journals
// and invoices, so the journals the reports aggregate are real double-entry
// postings rather than fixture rows) and then asserts the number the report
// returns — not merely that it answers.
//
// The seeding window is deliberately spread across three periods:
//
//	2025-01-01 .. 2025-12-31  prior financial year  (the comparative column)
//	2026-01-01 .. 2026-03-31  Q1 2026               (the reported period)
//	2026-04-01 .. 2026-06-30  Q2 2026               (must be excluded)

// ---------- fixtures ----------------------------------------------------------

// reportFixture is the shared ledger every test below asserts against.
type reportFixture struct {
	h         *appHarness
	customer  string
	supplier  string
	contactAR string // invoice with no due date
}

func seedReportFixture(t *testing.T) reportFixture {
	t.Helper()
	h := newHarness(t)
	resetLedger(t, h)
	f := reportFixture{
		h:        h,
		customer: createContact(t, h, "Parity customer "+uuid.NewString()[:6]),
		supplier: createContact(t, h, "Parity supplier "+uuid.NewString()[:6]),
	}

	// Prior financial year — these land in the comparative column only.
	postManualJournal(t, h, "2025-02-10T00:00:00Z", "py-rent",
		journalLine("469", "40"), journalLine("800", "-40")) // rent expense 40
	postManualJournal(t, h, "2025-03-05T00:00:00Z", "py-sales",
		journalLine("090", "200"), journalLine("200", "-200")) // sales income 200

	// Reported period (Q1 2026).
	postManualJournal(t, h, "2026-02-10T00:00:00Z", "q1-sales",
		journalLine("610", "100"), journalLine("200", "-100")) // sales income 100
	postManualJournal(t, h, "2026-03-15T00:00:00Z", "q1-rent",
		journalLine("469", "30"), journalLine("800", "-30")) // rent expense 30

	// After the reported period — must never appear in a Q1 report.
	postManualJournal(t, h, "2026-06-01T00:00:00Z", "q2-sales",
		journalLine("090", "700"), journalLine("200", "-700")) // sales income 700

	// Ageing fixture (as at 2026-03-31).
	postInvoice(t, h, "ACCREC", f.customer, "2026-01-05T00:00:00Z", "2026-02-05T00:00:00Z", "10") // 54 days
	postInvoice(t, h, "ACCREC", f.customer, "2026-03-01T00:00:00Z", "2026-03-15T00:00:00Z", "20") // 16 days
	postInvoice(t, h, "ACCREC", f.customer, "2026-03-25T00:00:00Z", "2026-04-10T00:00:00Z", "30") // not due
	postInvoice(t, h, "ACCREC", f.customer, "2025-06-01T00:00:00Z", "2025-06-01T00:00:00Z", "40") // 303 days: Older
	postInvoice(t, h, "ACCREC", f.customer, "2026-04-15T00:00:00Z", "2026-05-15T00:00:00Z", "99") // after as-at
	postInvoice(t, h, "ACCPAY", f.supplier, "2026-03-01T00:00:00Z", "2026-03-31T00:00:00Z", "70")

	// An authorised receivable with *no* due date (invoices.due_date is
	// nullable). It must still land in exactly one ageing bucket.
	f.contactAR = postInvoiceNoDueDate(t, h, f.customer, "2026-03-02T00:00:00Z", "55")

	return f
}

// resetLedger clears every posting the migration-seeded demo data left in the
// organisation, so the fixture below is the whole ledger the assertions in this
// file read. Each test provisions its own database (testutil.NewPool), so this
// touches nothing outside the calling test. The chart of accounts is left
// alone: the fixture posts against it by code, exactly as a client would.
func resetLedger(t *testing.T, h *appHarness) {
	t.Helper()
	ctx := context.Background()
	// gl_journal_lines, invoice_line_items and credit_note_allocations all
	// cascade from their parent, so the parents are the whole job.
	for _, stmt := range []string{
		`DELETE FROM gl_journals WHERE organisation_id = $1`,
		`DELETE FROM bank_transactions WHERE organisation_id = $1`,
		`DELETE FROM credit_notes WHERE organisation_id = $1`,
		`DELETE FROM invoices WHERE organisation_id = $1`,
	} {
		_, err := h.repos.Pool.Exec(ctx, stmt, seedDemoOrgID)
		require.NoError(t, err, stmt)
	}
}

// postManualJournal posts a balanced (or deliberately unbalanced) manual
// journal, so tests control the GL directly.
func postManualJournal(t *testing.T, h *appHarness, date, narration string, lines ...map[string]any) {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "/api/v1/manual-journals", map[string]any{
		"Narration":       narration,
		"Date":            date,
		"Status":          "POSTED",
		"LineAmountTypes": "NoTax",
		"JournalLines":    lines,
	}, true)
	require.Equalf(t, http.StatusCreated, status, "%s => %s", narration, string(body))
}

// journalLine is one manual-journal line: a debit is positive, a credit
// negative, exactly as the manual journal API takes LineAmount.
func journalLine(code, amount string) map[string]any {
	return map[string]any{"Description": "parity", "AccountCode": code, "LineAmount": amount}
}

// postInvoice posts an authorised invoice of `amount` (no tax) dated `date`
// with an explicit due date.
func postInvoice(t *testing.T, h *appHarness, invType, contactID, date, dueDate, amount string) string {
	t.Helper()
	body := map[string]any{
		"Type":            invType,
		"Status":          "AUTHORISED",
		"ContactID":       contactID,
		"Date":            date,
		"DueDate":         dueDate,
		"LineAmountTypes": "Exclusive",
		"LineItems": []map[string]any{
			invoiceLine(invType, amount),
		},
	}
	status, resp := h.do(t, http.MethodPost, "/api/v1/invoices", body, true)
	require.Equalf(t, http.StatusCreated, status, "%s => %s", invType, string(resp))
	return invoiceID(t, resp)
}

// postInvoiceNoDueDate omits DueDate entirely — the nullable column the ageing
// buckets used to drop on the floor.
func postInvoiceNoDueDate(t *testing.T, h *appHarness, contactID, date, amount string) string {
	t.Helper()
	status, resp := h.do(t, http.MethodPost, "/api/v1/invoices", map[string]any{
		"Type":            "ACCREC",
		"Status":          "AUTHORISED",
		"ContactID":       contactID,
		"Date":            date,
		"LineAmountTypes": "Exclusive",
		"LineItems":       []map[string]any{invoiceLine("ACCREC", amount)},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(resp))
	return invoiceID(t, resp)
}

func invoiceLine(invType, amount string) map[string]any {
	code := map[string]string{"ACCREC": "200", "ACCPAY": "400"}[invType]
	return map[string]any{
		"Description": "parity", "Quantity": "1", "UnitAmount": amount,
		"AccountCode": code, "TaxAmount": "0",
	}
}

func invoiceID(t *testing.T, resp []byte) string {
	t.Helper()
	var env struct {
		Invoices []struct {
			InvoiceID string `json:"InvoiceID"`
		} `json:"Invoices"`
	}
	require.NoError(t, json.Unmarshal(resp, &env))
	require.NotEmpty(t, env.Invoices)
	return env.Invoices[0].InvoiceID
}

// ---------- regression: aliased comparative accumulators ----------------------

// TestHTTP_Reports_ComparativeColumnsAreCorrect is the regression test for two
// pointer-aliasing defects in the repository: ProfitAndLoss handed all five
// comparative accumulators (income, gross, net, cost of sales, expenses) the
// *same* `*decimal.Decimal`, and BalanceSheet did the same with its four. The
// per-row comparative cells stayed correct, so the bug was invisible except in
// the section totals and headline figures — which is exactly what a reader
// trusts. It asserts the comparative column arithmetic, not just its presence.
func TestHTTP_Reports_ComparativeColumnsAreCorrect(t *testing.T) {
	f := seedReportFixture(t)

	// --- Profit & Loss: Q1 2026 against Q1 2025 ---------------------------
	pnl := fetchReport(t, f.h,
		"/api/v1/reports/profit-and-loss?fromDate=2026-01-01&toDate=2026-03-31"+
			"&compareFromDate=2025-01-01&compareToDate=2025-03-31")
	// Comparative column (cell index 2) of each headline row.
	assertMoney(t, "2025 income", comparativeByLabel(t, pnl, "Total income"), "200.00")
	assertMoney(t, "2025 gross profit", comparativeByLabel(t, pnl, "Gross Profit"), "200.00")
	assertMoney(t, "2025 operating expenses", comparativeByLabel(t, pnl, "Total less operating expenses"), "40.00")
	assertMoney(t, "2025 net profit", comparativeByLabel(t, pnl, "Net Profit"), "160.00")

	// Current column stays correct alongside it.
	// Q1 2026: 10 + 100 + 20 + 55 + 30 of sales = 215 income, and the 30 rent
	// plus the 70 advertising bill = 100 expenses.
	assertMoney(t, "2026 income", valueByLabel(t, pnl, "Total income"), "215.00")
	assertMoney(t, "2026 net profit", valueByLabel(t, pnl, "Net Profit"), "115.00")

	// The signature of the aliasing bug: every headline comparative collapsed
	// onto one running sum, so income == expenses == net profit. Distinct
	// values are the proof it is fixed.
	assert.NotEqual(t, comparativeByLabel(t, pnl, "Total income"), comparativeByLabel(t, pnl, "Net Profit"),
		"comparative income and net profit must not be the same variable")
	assert.NotEqual(t, comparativeByLabel(t, pnl, "Total less operating expenses"), comparativeByLabel(t, pnl, "Net Profit"),
		"comparative expenses and net profit must not be the same variable")

	// --- Balance Sheet: 2026-03-31 against 2025-12-31 ---------------------
	bs := fetchReport(t, f.h, "/api/v1/reports/balance-sheet?date=2026-03-31&compareDate=2025-12-31")

	// The comparative column must satisfy the same identity the report makes
	// on its own column: assets == liabilities + equity.
	cmpAssets := comparativeByLabel(t, bs, "Total Assets")
	cmpLiabilities := comparativeByLabel(t, bs, "Total Liabilities")
	cmpEquity := comparativeByLabel(t, bs, "Total Equity")
	cmpNet := comparativeByLabel(t, bs, "Net Assets")
	assertEqMoney(t, "comparative net assets == assets - liabilities", cmpNet, sub(cmpAssets, cmpLiabilities))
	assertEqMoney(t, "comparative net assets == equity", cmpNet, cmpEquity)

	// And every comparative total must equal the sum of the comparative cells
	// printed above it — the check that catches a total accumulating into the
	// wrong accumulator.
	assertEqMoney(t, "comparative total assets == sum of asset rows",
		cmpAssets, sumRowComparatives(t, bs, "Assets"))
	assertEqMoney(t, "comparative total liabilities == sum of liability rows",
		cmpLiabilities, sumRowComparatives(t, bs, "Liabilities"))
}

// ---------- regression: Executive Summary receivables were not date-bounded --

// TestHTTP_Reports_ExecutiveSummaryMatchesAgedTotals proves the Executive
// Summary's AR/AP KPIs are as-at figures: they used to aggregate every
// authorised invoice the organisation had ever raised, so an invoice dated
// after the report date inflated the KPI and the report disagreed with the
// aged report it drills into.
func TestHTTP_Reports_ExecutiveSummaryMatchesAgedTotals(t *testing.T) {
	f := seedReportFixture(t)

	exec := fetchReport(t, f.h, "/api/v1/reports/executive-summary?date=2026-03-31")
	execAR := valueByLabel(t, exec, "Accounts receivable")
	execAP := valueByLabel(t, exec, "Accounts payable")

	// Aged figures are the source of truth (as at 2026-03-31):
	//   10 + 20 + 30 + 40 + 55 = 155, and the 99.00 invoice dated 2026-04-15
	//   is after the as-at date and must not be counted.
	assertMoney(t, "executive summary AR", execAR, "155.00")
	assertMoney(t, "executive summary AP", execAP, "70.00")

	ar := fetchReport(t, f.h, "/api/v1/reports/aged-receivables?date=2026-03-31")
	ap := fetchReport(t, f.h, "/api/v1/reports/aged-payables?date=2026-03-31")
	assertEqMoney(t, "executive summary AR == aged receivables total", execAR, agedTotal(t, ar))
	assertEqMoney(t, "executive summary AP == aged payables total", execAP, agedTotal(t, ap))

	// Widen the as-at date and the KPI must move with it — the receipt of an
	// invoice after the old date, and nothing else.
	later := fetchReport(t, f.h, "/api/v1/reports/executive-summary?date=2026-04-30")
	assertMoney(t, "executive summary AR as at 2026-04-30", valueByLabel(t, later, "Accounts receivable"), "254.00")

	// The comparative column of the flow KPIs is unchanged by the fix.
	// The flow KPIs cover the month ending on `date` (March): 20 + 55 + 30 of
	// sales, since the February posting is outside it.
	assertMoney(t, "executive summary income", valueByLabel(t, exec, "Income"), "105.00")
}

// ---------- regression: ageing buckets dropped invoices without a due date ---

// TestHTTP_Reports_AgedBucketsPartitionTheTotal is the regression test for the
// ageing partition: the four age buckets were computed from `i.due_date`, which
// is nullable, so an authorised invoice with no due date appeared in the Total
// column and in no bucket — the report's own total did not equal the sum of the
// columns above it.
func TestHTTP_Reports_AgedBucketsPartitionTheTotal(t *testing.T) {
	f := seedReportFixture(t)

	ar := fetchReport(t, f.h, "/api/v1/reports/aged-receivables?date=2026-03-31")

	// Every contact row must satisfy total == sum of its buckets. Before the
	// fix the invoice without a due date sat in the total and in no bucket, so
	// the largest contact row was off by 55.00.
	var contactRows int
	totalRow := findRowByLabel(t, ar, "Total")
	for _, row := range allRows(ar) {
		if row.RowType != "Row" || len(row.Cells) < 7 {
			continue
		}
		contactRows++
		buckets := sumCells(row, 1, 5) // < 1 Month + 1 Month + 2 Months + 3 Months + Older
		assertEqMoney(t, "contact row "+cell(row, 0)+": total == sum of buckets", dec(cell(row, 6)), buckets)
	}
	require.NotZero(t, contactRows, "the seeded contacts must appear in the aged receivables report")

	// 10 (1 Month: 54 days past due) + 20 + 30 + 55 (< 1 Month: 16 days past
	// due, not yet due, and the no-due-date invoice aged from its invoice date)
	// + 40 (Older: 303 days) = 155. The 99.00 invoice is dated after the as-at
	// date.
	assertMoney(t, "aged receivables grand total", agedTotal(t, ar), "155.00")
	assertEqMoney(t, "grand total == sum of the grand-total buckets",
		agedTotal(t, ar), sumCells(totalRow, 1, 5))

	// The 55.00 invoice must have landed in a bucket, not been dropped: the
	// buckets of the affected contact would otherwise total 100.00.
	var withNoDueDate bool
	for _, row := range allRows(ar) {
		if row.RowType == "Row" && len(row.Cells) >= 7 && sumCells(row, 1, 5).String() == "100.00" {
			withNoDueDate = true
		}
	}
	require.False(t, withNoDueDate, "the invoice without a due date was left out of every bucket")

	// ?contactID narrows the report to one contact (the by-contact routes).
	mine := fetchReport(t, f.h, "/api/v1/reports/aged-receivables-by-contact?date=2026-03-31&contactID="+f.customer)
	assertMoney(t, "by-contact total", agedTotal(t, mine), "155.00")
	other := fetchReport(t, f.h, "/api/v1/reports/aged-receivables-by-contact?date=2026-03-31&contactID="+f.supplier)
	assertMoney(t, "by-contact total for a contact with no receivables", agedTotal(t, other), "0.00")
}

// ---------- date windows -----------------------------------------------------

// TestHTTP_Reports_DateWindowsNarrowTheNumbers drives two disjoint windows and
// asserts each returns only its own postings. Both bounds are probed: a
// posting from the prior year and one from the following quarter must be
// absent, and an account whose only activity is outside the window must report
// zero movement inside it.
func TestHTTP_Reports_DateWindowsNarrowTheNumbers(t *testing.T) {
	f := seedReportFixture(t)

	q1 := fetchReport(t, f.h, "/api/v1/reports/profit-and-loss?fromDate=2026-01-01&toDate=2026-03-31")
	q2 := fetchReport(t, f.h, "/api/v1/reports/profit-and-loss?fromDate=2026-04-01&toDate=2026-06-30")

	assertMoney(t, "Q1 income", valueByLabel(t, q1, "Total income"), "215.00")
	assertMoney(t, "Q2 income", valueByLabel(t, q2, "Total income"), "799.00")
	assertMoney(t, "Q1 net profit", valueByLabel(t, q1, "Net Profit"), "115.00")
	assertMoney(t, "Q2 net profit", valueByLabel(t, q2, "Net Profit"), "799.00")
	assert.NotEqual(t, valueByLabel(t, q1, "Total income"), valueByLabel(t, q2, "Total income"),
		"two disjoint windows must not return the same sum")

	// Account 469 (Rent) has activity in Q1 and in the prior year, and none at
	// all in Q2: its row must read zero for Q2 rather than carry a lifetime
	// total.
	assertMoney(t, "Q1 rent expense", accountRowValue(t, q1, "469", 1), "30.00")
	assertMoney(t, "Q2 rent expense", accountRowValue(t, q2, "469", 1), "0.00")

	// Account 800 (Accounts Payable) is credited in both seeded years; the
	// balance sheet bounds its cumulative balance by the as-at date.
	bs := fetchReport(t, f.h, "/api/v1/reports/balance-sheet?date=2026-03-31")
	// Assets 455 (200 cash + 255 receivable) == liabilities 140 + equity 315.
	assertMoney(t, "balance sheet as at 2026-03-31", valueByLabel(t, bs, "Net Assets"), "315.00")

	// Trial Balance: the period columns cover the window, YTD covers the
	// financial year to the same date — so Q1's March rent shows in the period
	// but a June posting could never appear in either.
	tb := fetchReport(t, f.h, "/api/v1/reports/trial-balance?date=2026-03-31&fromDate=2026-03-01")
	debits, credits := sumTrialBalanceTotals(t, tb)
	assertEqMoney(t, "trial balance period debits == credits", debits, credits)
	// March alone: 20 + 55 + 30 receivable, 70 advertising and 30 rent.
	assertMoney(t, "trial balance period debits cover the March postings only", debits, "205.00")
	// Year to date is wider: it picks up the February posting as well. The YTD
	// columns carry two measures — year-to-date movement for profit and loss
	// accounts, the balance carried as at the report date for balance sheet
	// accounts — so the property to hold them to is that the Total row is the
	// sum of the rows rendered above it (asserted below), not that the two
	// sides balance. The two sides balance only once a prior financial year's
	// result has been closed to equity; this fixture's prior year deliberately
	// has not been.
	assertTrialBalanceTotalIsTheSumOfItsRows(t, tb)
	assert.True(t, sumCells(findRowByLabel(t, tb, "Total"), 3, 4).f > debits.f,
		"YTD must be wider than the period movement")

	// Journals, account transactions and the general ledger are two-sided as
	// well: no posting outside the requested window may appear in any of them.
	assertRowsWithinDates(t, "journal report Q2",
		fetchReport(t, f.h, "/api/v1/reports/journal-report?fromDate=2026-04-01&toDate=2026-06-30"),
		"2026-04-01", "2026-06-30")
	assertRowsWithinDates(t, "account transactions Q1",
		fetchReport(t, f.h, "/api/v1/reports/account-transactions?fromDate=2026-01-01&toDate=2026-03-31"),
		"2026-01-01", "2026-03-31")
	assertRowsWithinDates(t, "general ledger detail Q1",
		fetchReport(t, f.h, "/api/v1/reports/general-ledger-detail?fromDate=2026-01-01&toDate=2026-03-31"),
		"2026-01-01", "2026-03-31")
}

// ---------- identities -------------------------------------------------------

// TestHTTP_Reports_AccountingIdentities holds every report to the identity it
// is supposed to satisfy, on the seeded organisation, including the
// comparative columns.
func TestHTTP_Reports_AccountingIdentities(t *testing.T) {
	f := seedReportFixture(t)

	// Trial Balance: the period columns are a double-entry movement, so the two
	// sides balance; and every column of the Total row is the sum of the rows
	// rendered above it, which is what lets a reader check the report against
	// the ledger rather than against itself.
	tb := fetchReport(t, f.h, "/api/v1/reports/trial-balance?date=2026-03-31")
	tbDebit, tbCredit := sumTrialBalanceTotals(t, tb)
	assertEqMoney(t, "trial balance period debits == credits", tbDebit, tbCredit)
	assertTrialBalanceTotalIsTheSumOfItsRows(t, tb)

	// Profit & Loss: income − cost of sales − expenses == net profit.
	pnl := fetchReport(t, f.h, "/api/v1/reports/profit-and-loss?fromDate=2026-01-01&toDate=2026-03-31&compare=true")
	income := valueByLabel(t, pnl, "Total income")
	expenses := valueByLabel(t, pnl, "Total less operating expenses")
	net := valueByLabel(t, pnl, "Net Profit")
	assertEqMoney(t, "P&L net profit == income − expenses", net, sub(income, expenses))
	assertEqMoney(t, "P&L comparative net profit == comparative income − comparative expenses",
		comparativeByLabel(t, pnl, "Net Profit"),
		sub(comparativeByLabel(t, pnl, "Total income"), comparativeByLabel(t, pnl, "Total less operating expenses")))

	// Balance Sheet: assets == liabilities + equity (equity folds in retained
	// earnings), and net assets == assets − liabilities.
	bs := fetchReport(t, f.h, "/api/v1/reports/balance-sheet?date=2026-03-31&compare=true")
	assets := valueByLabel(t, bs, "Total Assets")
	liabilities := valueByLabel(t, bs, "Total Liabilities")
	equity := valueByLabel(t, bs, "Total Equity")
	netAssets := valueByLabel(t, bs, "Net Assets")
	assertEqMoney(t, "balance sheet assets == liabilities + equity", assets, add(liabilities, equity))
	assertEqMoney(t, "balance sheet net assets == assets − liabilities", netAssets, sub(assets, liabilities))

	// Aged Receivables / Payables: the grand total is the sum of the buckets,
	// and it agrees with the AR/AP control figures on the balance sheet.
	ar := fetchReport(t, f.h, "/api/v1/reports/aged-receivables?date=2026-03-31")
	assertEqMoney(t, "aged receivables total == sum of buckets",
		agedTotal(t, ar), sumCells(findRowByLabel(t, ar, "Total"), 1, 5))
	ap := fetchReport(t, f.h, "/api/v1/reports/aged-payables?date=2026-03-31")
	assertEqMoney(t, "aged payables total == sum of buckets",
		agedTotal(t, ap), sumCells(findRowByLabel(t, ap, "Total"), 1, 5))

	// Bank Summary: opening + received − spent == closing.
	bank := fetchReport(t, f.h, "/api/v1/reports/bank-summary?fromDate=2026-01-01&toDate=2026-03-31")
	var bankRows int
	for _, row := range allRows(bank) {
		if row.RowType != "Row" || len(row.Cells) < 5 {
			continue
		}
		bankRows++
		opening, received, spent, closing := dec(cell(row, 1)), dec(cell(row, 2)), dec(cell(row, 3)), dec(cell(row, 4))
		assertEqMoney(t, "bank summary opening + received − spent == closing",
			closing, sub(add(opening, received), spent))
	}
	require.NotZero(t, bankRows, "the chart of accounts always carries at least one bank account")

	// Sales Tax / BAS: the report's Total row must equal the per-rate lines,
	// column by column.
	bas := fetchReport(t, f.h, "/api/v1/reports/bas?fromDate=2026-01-01&toDate=2026-03-31")
	basTotal := findRowByLabel(t, bas, "Total")
	for col := 1; col <= 5; col++ {
		var summed decimalLike
		for _, row := range allRows(bas) {
			if row.RowType != "Row" {
				continue
			}
			summed = add(summed, dec(cell(row, col)))
		}
		assertEqMoney(t, "BAS total column "+strconv.Itoa(col), dec(cell(basTotal, col)), summed)
	}
}

// ---------- envelope ---------------------------------------------------------

// TestHTTP_Reports_IndexIsACatalogue asserts the index is usable on its own:
// every entry names a report and the path that serves it.
func TestHTTP_Reports_IndexIsACatalogue(t *testing.T) {
	f := seedReportFixture(t)

	// `/reports` is the index the frontend renders, not a Xero report: it lists
	// the implemented reports rather than wrapping one. (Xero's own GET
	// /Reports answers with a multi-report envelope; see the audit doc.)
	status, body := f.h.do(t, http.MethodGet, "/api/v1/reports", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var idx struct {
		Reports []struct {
			ReportID   string `json:"ReportID"`
			ReportName string `json:"ReportName"`
			Path       string `json:"Path"`
		} `json:"Reports"`
	}
	require.NoError(t, json.Unmarshal(body, &idx))
	require.NotEmpty(t, idx.Reports)
	for _, r := range idx.Reports {
		assert.NotEmpty(t, r.ReportID)
		assert.NotEmpty(t, r.ReportName)
		assert.NotEmpty(t, r.Path)
	}
}

// aliasRoutes maps a registered alias path to the path the index advertises for
// the same report. An alias is a convenience second path, not a second report.
var aliasRoutes = map[string]string{
	"/api/v1/reports/profit-loss":    "/api/v1/reports/profit-and-loss",
	"/api/v1/reports/general-ledger": "/api/v1/reports/general-ledger-detail",
}

// nonReportRoutes are the routes served under /reports/ that deliberately do not
// answer with a report, each with the reason. Keeping them out of the index is
// what stops a client that walks the index from asking for a report and being
// handed something else.
var nonReportRoutes = map[string]string{
	"/api/v1/reports/invoice-summary": "the invoices and sales screens' own KPI object",
}

// nonReportShape is the documented shape of each non-report route: the fields a
// client of that route relies on. Pinning them here makes "not a report" a
// contract rather than an accident.
var nonReportShape = map[string][]string{
	"/api/v1/reports/invoice-summary": {
		"totalInvoices", "draft", "authorised", "paid", "overdue", "totalDue", "totalPaid",
	},
}

// TestHTTP_Reports_EveryEndpointAnswersInTheXeroEnvelope walks every report
// route the application registers and asserts the response is the canonical
// Xero Reporting API envelope the SDKs parse.
//
// The route list is enumerated from the router rather than hand-written, and
// the index at /reports is checked against it in both directions, so the index
// can never advertise a report the routes do not serve and a new route under
// /reports/ can never appear without being classified:
//
//	the index        advertises the report that path serves
//	aliasRoutes      a second path to a report the index already advertises
//	nonReportRoutes  a route under /reports/ that deliberately is not a report
func TestHTTP_Reports_EveryEndpointAnswersInTheXeroEnvelope(t *testing.T) {
	f := seedReportFixture(t)

	// Every window parameter any report understands, in one query string: each
	// handler reads the ones it needs and ignores the rest.
	const window = "?date=2026-03-31&fromDate=2026-01-01&toDate=2026-03-31"

	routes := registeredReportRoutes(t, f.h)

	status, body := f.h.do(t, http.MethodGet, "/api/v1/reports", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var idx struct {
		Reports []struct {
			ReportID   string `json:"ReportID"`
			ReportName string `json:"ReportName"`
			Path       string `json:"Path"`
		} `json:"Reports"`
	}
	require.NoError(t, json.Unmarshal(body, &idx))
	require.NotEmpty(t, idx.Reports)

	// advertised maps the route the index points at to the ReportID it promises.
	advertised := map[string]string{}
	for _, r := range idx.Reports {
		assert.NotEmpty(t, r.ReportID)
		assert.NotEmpty(t, r.ReportName)
		require.Truef(t, strings.HasPrefix(r.Path, "/reports/"), "index entry %q is not under /reports", r.Path)
		advertised["/api/v1"+r.Path] = r.ReportID
	}

	// First way the index and the routes can disagree: an entry pointing at a
	// path no route serves.
	for path := range advertised {
		_, ok := routes[path]
		assert.Truef(t, ok, "the index advertises %s but no route serves it", path)
	}
	// Second way: a route nobody classified — a report missing from the index,
	// or a non-report that crept in without being named as one.
	for path := range routes {
		_, isAdvertised := advertised[path]
		canonical, isAlias := aliasRoutes[path]
		reason, isNonReport := nonReportRoutes[path]
		if isAlias {
			_, canonicalIsAdvertised := advertised[canonical]
			assert.Truef(t, canonicalIsAdvertised, "%s is an alias of %s, which the index does not advertise", path, canonical)
		}
		if isNonReport {
			assert.NotEmptyf(t, reason, "%s must carry the reason it is not a report", path)
		}
		assert.Truef(t, isAdvertised || isAlias || isNonReport,
			"%s is registered under /reports/ but classified nowhere: add it to the index, to aliasRoutes, or to nonReportRoutes with a reason", path)
	}

	served := map[string]string{} // route path -> the ReportID it actually serves
	for path := range routes {
		status, body := f.h.do(t, http.MethodGet, path+window, nil, true)
		require.Equalf(t, http.StatusOK, status, "%s => %s", path, string(body))

		if reason, ok := nonReportRoutes[path]; ok {
			// It must not answer like a report — that is the point of keeping it
			// out of the index — and its own shape must still be the documented
			// one, so the exemption cannot silently rot.
			var bare map[string]json.RawMessage
			require.NoErrorf(t, json.Unmarshal(body, &bare), "%s is not JSON: %s", path, string(body))
			assert.NotContainsf(t, bare, "Reports", "%s serves %s, not a report envelope", path, reason)
			for _, key := range nonReportShape[path] {
				assert.Containsf(t, bare, key, "%s no longer serves %q: nonReportShape is stale", path, key)
			}
			continue
		}

		var env struct {
			ID           string `json:"Id"`
			Status       string `json:"Status"`
			ProviderName string `json:"ProviderName"`
			DateTimeUTC  string `json:"DateTimeUTC"`
			Reports      []struct {
				ReportID     string `json:"ReportID"`
				ReportName   string `json:"ReportName"`
				ReportType   string `json:"ReportType"`
				ReportTitles []string
				ReportDate   string `json:"ReportDate"`
				Rows         []struct {
					RowType string `json:"RowType"`
					Cells   []struct {
						Value string `json:"Value"`
					} `json:"Cells"`
				} `json:"Rows"`
			} `json:"Reports"`
		}
		require.NoErrorf(t, json.Unmarshal(body, &env), "%s is not the Xero envelope", path)

		assert.NotEmpty(t, env.ID, path)
		assert.Equal(t, "OK", env.Status, path)
		assert.Equal(t, "goxero", env.ProviderName, path)
		_, err := time.Parse(time.RFC3339, env.DateTimeUTC)
		assert.NoErrorf(t, err, "%s: DateTimeUTC must be RFC3339", path)
		require.Lenf(t, env.Reports, 1, "%s must return exactly one report", path)
		rep := env.Reports[0]
		assert.NotEmpty(t, rep.ReportID, path)
		assert.NotEmpty(t, rep.ReportName, path)
		assert.NotEmpty(t, rep.ReportType, path)
		assert.NotEmpty(t, rep.Rows, "%s returned no rows", path)
		for i, row := range rep.Rows {
			assert.Containsf(t, []string{"Header", "Section", "Row", "SummaryRow"}, row.RowType,
				"%s: row %d has an unknown RowType %q", path, i, row.RowType)
			if row.RowType == "Section" {
				continue // sections carry a title and nest their own rows
			}
			assert.NotEmptyf(t, row.Cells, "%s: row %d has no cells", path, i)
		}
		served[path] = rep.ReportID

		// A route the index advertises must serve the report its entry names.
		if want, ok := advertised[path]; ok {
			assert.Equalf(t, want, rep.ReportID, "%s: the index advertises ReportID %q", path, want)
		}
	}

	// An alias must serve exactly the report its canonical path serves.
	for path, canonical := range aliasRoutes {
		assert.Equalf(t, served[canonical], served[path], "%s and %s must serve the same report", path, canonical)
	}
}

// registeredReportRoutes enumerates the GET routes the application serves under
// /api/v1/reports/ from the router itself, keyed by path. Enumerating rather
// than hand-listing is what stops a new report route from escaping the envelope
// test above.
func registeredReportRoutes(t *testing.T, h *appHarness) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range h.app.GetRoutes() {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.Path, "/api/v1/reports/") {
			continue
		}
		out[r.Path] = r.Name
	}
	require.NotEmpty(t, out, "the app registers no /reports/ routes")
	return out
}

// ---------- helpers ----------------------------------------------------------

// The existing suite carries a float64 shim (decimalLike). These helpers reuse
// it so assertions read as money comparisons rather than string equality where
// the exact decimal representation is not the point.

func add(a, b decimalLike) decimalLike { return decimalLike{f: a.f + b.f} }
func sub(a, b decimalLike) decimalLike { return decimalLike{f: a.f - b.f} }

func assertEqMoney(t *testing.T, what string, got, want decimalLike) {
	t.Helper()
	assert.Truef(t, sub(got, want).Abs().LessThan(dec("0.005")), "%s: got %s, want %s", what, got, want)
}

func assertMoney(t *testing.T, what string, got decimalLike, want string) {
	t.Helper()
	assert.Equal(t, want, got.String(), what)
}

// valueByLabel returns the amount in the first amount column of the named
// summary row.
func valueByLabel(t *testing.T, env xeroReportEnvelope, label string) decimalLike {
	t.Helper()
	return cellByLabel(t, env, label, 1)
}

// comparativeByLabel returns the amount in the *comparative* column of the
// named summary row.
func comparativeByLabel(t *testing.T, env xeroReportEnvelope, label string) decimalLike {
	t.Helper()
	return cellByLabel(t, env, label, 2)
}

func cellByLabel(t *testing.T, env xeroReportEnvelope, label string, col int) decimalLike {
	t.Helper()
	row := findRowByLabel(t, env, label)
	require.GreaterOrEqualf(t, len(row.Cells), col+1, "row %q has no column %d", label, col)
	return dec(row.Cells[col].Value)
}

func findRowByLabel(t *testing.T, env xeroReportEnvelope, label string) xeroReportRow {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	var walk func(rows []xeroReportRow) (xeroReportRow, bool)
	walk = func(rows []xeroReportRow) (xeroReportRow, bool) {
		for _, r := range rows {
			if len(r.Cells) > 0 && r.Cells[0].Value == label {
				return r, true
			}
			if v, ok := walk(r.Rows); ok {
				return v, true
			}
		}
		return xeroReportRow{}, false
	}
	row, ok := walk(env.Reports[0].Rows)
	require.Truef(t, ok, "no report row labelled %q", label)
	return row
}

// accountRowValue returns the amount column of the row whose first cell names
// the account with `code`.
func accountRowValue(t *testing.T, env xeroReportEnvelope, code string, col int) decimalLike {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	var walk func(rows []xeroReportRow) (xeroReportRow, bool)
	walk = func(rows []xeroReportRow) (xeroReportRow, bool) {
		for _, r := range rows {
			if len(r.Cells) > 0 && strings.Contains(r.Cells[0].Value, "("+code+")") {
				return r, true
			}
			if v, ok := walk(r.Rows); ok {
				return v, true
			}
		}
		return xeroReportRow{}, false
	}
	row, ok := walk(env.Reports[0].Rows)
	require.Truef(t, ok, "no report row for account %s", code)
	require.GreaterOrEqualf(t, len(row.Cells), col+1, "account %s has no column %d", code, col)
	return dec(row.Cells[col].Value)
}

// sectionRows returns the account rows of the named section.
func sectionRows(t *testing.T, env xeroReportEnvelope, title string) []xeroReportRow {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	for _, r := range env.Reports[0].Rows {
		if r.RowType == "Section" && r.Title == title {
			return r.Rows
		}
	}
	t.Fatalf("no section titled %q", title)
	return nil
}

// sumRowComparatives adds the comparative cell (column 2) of every Row inside
// the named section, skipping the section's trailing SummaryRow.
func sumRowComparatives(t *testing.T, env xeroReportEnvelope, section string) decimalLike {
	t.Helper()
	out := dec("")
	for _, r := range sectionRows(t, env, section) {
		if r.RowType != "Row" {
			continue
		}
		out = add(out, dec(cell(r, 2)))
	}
	return out
}

// allRows flattens a report into a depth-first walk of every row, sections
// included.
func allRows(env xeroReportEnvelope) []xeroReportRow {
	if len(env.Reports) == 0 {
		return nil
	}
	var out []xeroReportRow
	var walk func(rows []xeroReportRow)
	walk = func(rows []xeroReportRow) {
		for _, r := range rows {
			out = append(out, r)
			walk(r.Rows)
		}
	}
	walk(env.Reports[0].Rows)
	return out
}

// agedTotal reads the grand-total row of an aged report (its last cell).
func agedTotal(t *testing.T, env xeroReportEnvelope) decimalLike {
	t.Helper()
	rows := allRows(env)
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if r.RowType == "SummaryRow" && len(r.Cells) > 0 && r.Cells[0].Value == "Total" {
			return dec(cell(r, len(r.Cells)-1))
		}
	}
	t.Fatal("aged report has no Total row")
	return dec("")
}

// assertTrialBalanceTotalIsTheSumOfItsRows reads every rendered account row
// back out of the payload and checks each of the four amount columns of the
// Total row against the sum of that column. It is the property the defect was
// about: the Total used to be accumulated independently of what was rendered,
// so it agreed with the ledger and not with the report.
func assertTrialBalanceTotalIsTheSumOfItsRows(t *testing.T, env xeroReportEnvelope) {
	t.Helper()
	total := findRowByLabel(t, env, "Total")
	require.GreaterOrEqualf(t, len(total.Cells), 5, "trial balance Total row is missing columns")

	names := []string{"Debit", "Credit", "YTD Debit", "YTD Credit"}
	sums := make([]decimalLike, 4)
	var accountRows int
	for _, row := range allRows(env) {
		if row.RowType != "Row" || len(row.Cells) != 5 {
			continue // header, section titles, summary rows
		}
		accountRows++
		for col := 1; col <= 4; col++ {
			sums[col-1] = add(sums[col-1], dec(cell(row, col)))
		}
	}
	require.NotZerof(t, accountRows, "the trial balance rendered no account rows to check")
	for col := 1; col <= 4; col++ {
		assertEqMoney(t, "trial balance Total "+names[col-1]+" == sum of the rendered rows",
			dec(cell(total, col)), sums[col-1])
	}
}

// everyRowDate checks each row's date cell falls inside [from, to].
func assertRowsWithinDates(t *testing.T, what string, env xeroReportEnvelope, from, to string) {
	t.Helper()
	var dated int
	for _, r := range allRows(env) {
		if r.RowType != "Row" || len(r.Cells) == 0 {
			continue
		}
		d := r.Cells[0].Value
		if _, err := time.Parse("2006-01-02", d); err != nil {
			continue // not a dated row (opening balance, total, …)
		}
		dated++
		assert.Truef(t, d >= from && d <= to, "%s: row dated %s is outside %s..%s", what, d, from, to)
	}
	require.NotZerof(t, dated, "%s: no dated rows to check", what)
}

func cell(row xeroReportRow, col int) string {
	if col >= len(row.Cells) {
		return ""
	}
	return row.Cells[col].Value
}

func sumCells(row xeroReportRow, from, to int) decimalLike {
	out := dec("")
	for col := from; col <= to; col++ {
		out = add(out, dec(cell(row, col)))
	}
	return out
}

// ---------- regression: an ordering key that was not total --------------------

// TestHTTP_Reports_GlDetailLineOrderIsStable is the regression test for the
// missing tiebreaker in journalFeed. Its ordering key was (account code, journal
// date, journal id); two lines of one journal posting to one account on one date
// tie on all three, so the physical heap order decided which came first — and
// General Ledger Detail accumulates its running Balance column in the order it
// receives, so the same URL printed a different balance for the same row on two
// calls. line_id is now the last key, which makes the order total.
//
// The fixture makes the defect far more than a coin flip: three journals each
// post three different amounts to account 469 on one date, so a report that fell
// back to heap order inside a journal would satisfy the assertion only about
// 1/(3!^3) of the time.
func TestHTTP_Reports_GlDetailLineOrderIsStable(t *testing.T) {
	f := seedReportFixture(t)

	const (
		day   = "2026-03-20T00:00:00Z"
		path  = "/api/v1/reports/general-ledger-detail?fromDate=2026-03-01&toDate=2026-03-31"
		query = `
			SELECT l.net_amount::text
			  FROM gl_journal_lines l
			  JOIN gl_journals j ON j.journal_id = l.journal_id
			  JOIN accounts a     ON a.account_id = l.account_id
			 WHERE j.organisation_id = $1
			   AND a.code = '469'
			   AND j.journal_date BETWEEN $2 AND $3
			 ORDER BY a.code, j.journal_date, j.journal_id, l.line_id`
	)
	for i := 0; i < 3; i++ {
		postManualJournal(t, f.h, day, "tie-"+strconv.Itoa(i),
			journalLine("469", strconv.Itoa(i*10+1)),
			journalLine("469", strconv.Itoa(i*10+2)),
			journalLine("469", strconv.Itoa(i*10+3)),
			journalLine("800", strconv.Itoa(-(i*30+6))),
		)
	}

	// The contract: a tie is broken by line_id, ascending.
	rows, err := f.h.repos.Pool.Query(context.Background(), query,
		seedDemoOrgID, "2026-03-01", "2026-03-31")
	require.NoError(t, err)
	var want []decimalLike
	for rows.Next() {
		var amount string
		require.NoError(t, rows.Scan(&amount))
		want = append(want, dec(amount))
	}
	rows.Close()
	require.NoError(t, rows.Err())
	require.Len(t, want, 10, "9 tied lines plus the fixture's own 469 rent line")

	got := accountSectionNetAmounts(t, fetchReport(t, f.h, path), "469")
	assert.Equal(t, want, got, "tied lines must come back in line_id order")

	// And the next call must repeat it, which is the property the running
	// Balance column actually depends on.
	assert.Equal(t, got, accountSectionNetAmounts(t, fetchReport(t, f.h, path), "469"))
}

// accountSectionNetAmounts returns the net amount (debit minus credit) of every
// posted line in one account's block of the General Ledger Detail, in the order
// the report renders them. The block's synthetic "Opening Balance" row and its
// closing SummaryRow are not posted lines and are skipped.
func accountSectionNetAmounts(t *testing.T, env xeroReportEnvelope, code string) []decimalLike {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	suffix := "(" + code + ")"
	for _, r := range env.Reports[0].Rows {
		if r.RowType != "Section" || !strings.HasSuffix(r.Title, suffix) {
			continue
		}
		var out []decimalLike
		for _, line := range r.Rows {
			if line.RowType != "Row" || cell(line, 3) == "Opening Balance" {
				continue
			}
			out = append(out, dec(cell(line, 4)).Sub(dec(cell(line, 5))))
		}
		return out
	}
	t.Fatalf("no account section for %s", code)
	return nil
}
