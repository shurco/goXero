package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is the regression suite for the three defects
// docs/sales-tax-tax-amount.md section 5.2 measures in the sales-tax workpaper,
// each of which leaves the report unable to print a tax figure at all:
//
//	(b)(i)   "measured" was tax_amount <> 0, so a 0%-rated line whose correct
//	         tax is zero could never be measured and blocked its side forever
//	(b)(ii)  the Total row required every rate the ledger uses to have measured
//	         the side, including rates with no lines on it at all — whose cell
//	         is empty by construction — so no multi-rate organisation could
//	         reach a tax total
//	(b)(iii) the purchase side was restricted to the cost classes, which do
//	         not include FIXED, so the input tax on capital accounts was
//	         dropped from Tax Paid and Net Tax overstated the liability
//
// Each test posts its own ledger and reads the report over the API, so the
// assertion is about what a reader sees rather than about the query behind it.

// TestHTTP_Reports_SalesTaxZeroRatedLineIsMeasured is (b)(i): a 0% rate's
// answer is zero, so a line under it with tax_amount 0 is a measured zero, not
// a missing value. It must not hold its side — or the total over it — back.
func TestHTTP_Reports_SalesTaxZeroRatedLineIsMeasured(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)

	postManualJournal(t, h, "2026-04-10T00:00:00Z", "exempt-sale",
		taxedJournalLine("200", "-100", "NONE", "0"), journalLine("610", "100"))
	postManualJournal(t, h, "2026-04-11T00:00:00Z", "exempt-purchase",
		taxedJournalLine("400", "60", "NONE", "0"), journalLine("800", "-60"))

	env := fetchReport(t, h, "/api/v1/reports/sales-tax?fromDate=2026-04-01&toDate=2026-04-30")
	rows := amountRows(env)
	require.Len(t, rows, 1, "one rate was posted")
	assert.Equal(t, taxRowLabel(t, h, "NONE"), cell(rows[0], 0))
	assert.Equal(t, "0.00", cell(rows[0], 3),
		"a 0% rate's tax is a measured zero and must be printed, not left empty")
	assert.Equal(t, "0.00", cell(rows[0], 4),
		"a 0% rate's tax is a measured zero and must be printed, not left empty")
	assert.Equal(t, "0.00", cell(rows[0], 5), "both sides of a 0% rate are measured")

	total := findRowByLabel(t, env, "Total")
	assert.Equal(t, "Total", cell(total, 0), "every rate measured both sides, so there is nothing to cover")
	assert.Equal(t, "0.00", cell(total, 3))
	assert.Equal(t, "0.00", cell(total, 4))
	assert.Equal(t, "0.00", cell(total, 5))
	assert.NotContains(t, strings.Join(rowLabels(env), " "), "with no determined tax",
		"no line is short")
}

// TestHTTP_Reports_SalesTaxTotalSpansRatesWithLinesOnTheSide is (b)(ii): a rate
// that never posted to a side has nothing to measure there, so it must not
// stop that side from being totalled.
func TestHTTP_Reports_SalesTaxTotalSpansRatesWithLinesOnTheSide(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)

	postManualJournal(t, h, "2026-05-04T00:00:00Z", "sale",
		taxedJournalLine("200", "-100", "OUTPUT", "8.25"), journalLine("610", "100"))
	postManualJournal(t, h, "2026-05-05T00:00:00Z", "purchase",
		taxedJournalLine("400", "50", "INPUT", "4.12"), journalLine("800", "-50"))

	env := fetchReport(t, h, "/api/v1/reports/sales-tax?fromDate=2026-05-01&toDate=2026-05-31")
	rows := amountRows(env)
	require.Len(t, rows, 2, "two rates were posted, one on each side")

	total := findRowByLabel(t, env, "Total")
	assert.Equal(t, "Total", cell(total, 0),
		"OUTPUT posted no purchase and INPUT posted no sale: neither holds the other side's total back")
	assertMoney(t, "tax collected over the one rate that has sales", dec(cell(total, 3)), "8.25")
	assertMoney(t, "tax paid over the one rate that has purchases", dec(cell(total, 4)), "4.12")
	assertMoney(t, "net tax", dec(cell(total, 5)), "4.13")
}

// TestHTTP_Reports_SalesTaxCapitalInputTaxIsTaxPaid is (b)(iii): FIXED is a
// capital class and is not a P&L cost, so the net columns rightly exclude it —
// but the tax columns are a tax workpaper and the input tax on a capital
// account is tax paid like any other.
func TestHTTP_Reports_SalesTaxCapitalInputTaxIsTaxPaid(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)

	// 710 Office Equipment is FIXED: an asset, not a cost the P&L carries.
	postManualJournal(t, h, "2026-06-10T00:00:00Z", "equipment",
		taxedJournalLine("710", "1000", "INPUT", "82.50"), journalLine("800", "-1000"))

	env := fetchReport(t, h, "/api/v1/reports/sales-tax?fromDate=2026-06-01&toDate=2026-06-30")
	rows := amountRows(env)
	require.Len(t, rows, 1, "one rate was posted")
	assertMoney(t, "net purchases stays a profit-and-loss column", dec(cell(rows[0], 2)), "0.00")
	assertMoney(t, "the input tax on a capital account is still tax paid",
		dec(cell(rows[0], 4)), "82.50")

	total := findRowByLabel(t, env, "Total")
	assertMoney(t, "the capital input tax reaches the total", dec(cell(total, 4)), "82.50")
	assertMoney(t, "net purchases total excludes capital", dec(cell(total, 2)), "0.00")
}

// TestHTTP_Reports_SalesTaxShortRateIsStatedNotAdjusted is the other half of
// (b)(ii) and the shape of the coordinator's product decision, restated after
// the first implementation of it was measured against a live ledger. A line
// whose tax is not determined records 0.00, so it adds nothing to its rate's
// sum: the sum is the whole of the tax the ledger records and there is no
// partial figure to withhold. Emptying that rate's cell therefore hid every
// other line under it — on the shared dev database one such line took 1,971.46
// out of a column whose remaining lines record exactly that — so the report now
// prints the ledger's own figure, names the rate and the line count the ledger
// records no tax for, and leaves the amount alone.
func TestHTTP_Reports_SalesTaxShortRateIsStatedNotAdjusted(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)

	postManualJournal(t, h, "2026-07-04T00:00:00Z", "sale",
		taxedJournalLine("200", "-100", "OUTPUT", "8.25"), journalLine("610", "100"))
	postManualJournal(t, h, "2026-07-05T00:00:00Z", "purchase-measured",
		taxedJournalLine("400", "50", "OUTPUT2", "4.38"), journalLine("800", "-50"))
	postManualJournal(t, h, "2026-07-06T00:00:00Z", "purchase-measured-too",
		taxedJournalLine("400", "50", "INPUT", "4.12"), journalLine("800", "-50"))
	// The shape a bank coding leaves behind when it records a rate but no tax:
	// a rate, a zero, and no document that ever supplied the figure.
	postManualJournal(t, h, "2026-07-07T00:00:00Z", "purchase-undetermined",
		taxedJournalLine("400", "50", "INPUT", "0"), journalLine("800", "-50"))

	env := fetchReport(t, h, "/api/v1/reports/sales-tax?fromDate=2026-07-01&toDate=2026-07-31")

	input := findRowByLabel(t, env, taxRowLabel(t, h, "INPUT"))
	assertMoney(t, "a rate's column is the whole of the tax the ledger records under it",
		dec(cell(input, 4)), "4.12")
	assertMoney(t, "and so is its net", dec(cell(input, 5)), "-4.12")

	total := salesTaxTotalRow(t, env)
	assert.Equal(t, "Total (1 tax-typed line records no tax)", cell(total, 0),
		"a total the ledger holds no tax for a line of is qualified, not silently printed")
	assertMoney(t, "tax collected", dec(cell(total, 3)), "8.25")
	assertMoney(t, "tax paid, the short rate included at what it records",
		dec(cell(total, 4)), "8.50")
	assertMoney(t, "net tax", dec(cell(total, 5)), "-0.25")

	caveat := strings.Join(rowLabels(env), " ")
	assert.Contains(t, caveat, "The ledger records no tax at all for Tax Paid: 1 line naming a rate",
		"the caveat states the ledger's gap in lines")
	assert.Contains(t, caveat, taxRowLabel(t, h, "INPUT"),
		"the caveat names the rate it is under")
	assert.Contains(t, caveat, "contribute nothing to the figures above",
		"the caveat says what the gap does to the figures")
}

// salesTaxTotalRow returns the workpaper's Total row. It is looked up as a
// SummaryRow rather than by its label, because where the ledger records no tax
// for a tax-typed line the label itself carries the qualifier and is no longer
// the plain "Total".
func salesTaxTotalRow(t *testing.T, env xeroReportEnvelope) xeroReportRow {
	t.Helper()
	for _, row := range allRows(env) {
		if row.RowType == "SummaryRow" && len(row.Cells) == 6 {
			return row
		}
	}
	require.Fail(t, "the workpaper has no Total row")
	return xeroReportRow{}
}

// TestHTTP_Reports_SalesTaxMatchesTheReferenceDataset is the acceptance test.
// It runs against the organisation exactly as migrations 00023 and 00024
// imported it — the frozen Xero reference capture, with no other writer — and
// asserts the three figures docs/sales-tax-tax-amount.md section 6.5 derives
// for it, rate by rate and in total, plus the identity the report exists to
// satisfy: Net Tax is account 820 Sales Tax's own balance for the period, read
// from the ledger rather than from the report.
func TestHTTP_Reports_SalesTaxMatchesTheReferenceDataset(t *testing.T) {
	h := newHarness(t)

	env := fetchReport(t, h, "/api/v1/reports/sales-tax?fromDate=2026-01-01&toDate=2026-12-31")
	total := salesTaxTotalRow(t, env)
	assert.Equal(t, "Total", cell(total, 0),
		"every rate that posted to a side measures it, so the total is complete")
	assertMoney(t, "tax collected", dec(cell(total, 3)), "2437.80")
	assertMoney(t, "tax paid", dec(cell(total, 4)), "2015.21")
	assertMoney(t, "net tax", dec(cell(total, 5)), "422.59")

	// Per rate, against Xero's own captured tax column
	// (docs/xero-reference/general-ledger-detail.txt, section 7): each of these
	// is the net of the rate and they sum to the total above.
	for _, want := range []struct {
		taxType   string
		collected string
		paid      string
		net       string
	}{
		// A cell is empty where the rate has no lines on that side — there is no
		// tax the ledger could hold for it — and every row carries a Net Tax, so
		// the four nets sum to the total the row above them prints.
		{"OUTPUT", "2423.47", "", "2423.47"},
		{"OUTPUT2", "14.33", "43.75", "-29.42"},
		{"INPUT", "", "1971.46", "-1971.46"},
		{"NONE", "", "0.00", "0.00"},
	} {
		row := findRowByLabel(t, env, taxRowLabel(t, h, want.taxType))
		assert.Equal(t, want.collected, cell(row, 3), want.taxType+" Tax Collected")
		assert.Equal(t, want.paid, cell(row, 4), want.taxType+" Tax Paid")
		assert.Equal(t, want.net, cell(row, 5), want.taxType+" Net Tax")
	}

	// The four rows' own nets add up to the total, rate by rate, which is what
	// lets a reader find a rate's contribution without re-deriving it.
	var netSum decimalLike
	for _, taxType := range []string{"OUTPUT", "OUTPUT2", "INPUT", "NONE"} {
		netSum = add(netSum, dec(cell(findRowByLabel(t, env, taxRowLabel(t, h, taxType)), 5)))
	}
	assertEqMoney(t, "the rates' net taxes sum to the total", dec(cell(total, 5)), netSum)

	// The reconciliation the workpaper is for: the report derived 422.59, and
	// the tax control account holds it.
	var balance string
	require.NoError(t, h.repos.Pool.QueryRow(context.Background(), `
		SELECT COALESCE(SUM(l.net_amount), 0)::text
		  FROM gl_journal_lines l
		  JOIN gl_journals j ON j.journal_id = l.journal_id
		  JOIN accounts a ON a.account_id = l.account_id
		 WHERE j.organisation_id = $1 AND a.code = '820'
		   AND j.journal_date BETWEEN DATE '2026-01-01' AND DATE '2026-12-31'`,
		seedDemoOrgID).Scan(&balance))
	assertMoney(t, "account 820 Sales Tax for the period", dec(balance), "-422.59")
	assertEqMoney(t, "Net Tax == the credit in account 820",
		dec(cell(total, 5)), sub(decimalLike{}, dec(balance)))
}
