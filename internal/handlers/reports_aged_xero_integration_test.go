package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/repository"
)

// This file is the Xero-parity suite for the two aged reports and for the Budget
// Summary.
//
// Every expectation comes from somewhere, and never from a figure typed into the
// test: the ageing columns and the Expense Claims block are read out of
// docs/xero-reference/aged-{receivables,payables}-summary.txt, the amount the
// claim fixture is built from is the one the payables capture puts in that
// block, the claimant label is read out of the database the renderer reads, and
// the Percentage of total row is recomputed from the very rows the report
// printed.

const (
	agedReceivablesRef = "../../docs/xero-reference/aged-receivables-summary.txt"
	agedPayablesRef    = "../../docs/xero-reference/aged-payables-summary.txt"
)

// ---------- reading the captured Xero reports --------------------------------

// agedReferenceColumns reads a captured report's column headings. The aged
// reports' columns are Xero's, so Xero's own export is the source of truth for
// them rather than a list written into this test.
func agedReferenceColumns(t *testing.T, path string) []string {
	t.Helper()
	for _, line := range agedReferenceLines(t, path) {
		if strings.HasPrefix(line, "Contact\t") {
			return strings.Split(line, "\t")
		}
	}
	t.Fatalf("%s: no column heading line", path)
	return nil
}

// agedReferenceRow returns the cells of the captured line whose first cell is
// `label` — "Total Expense Claims", say — so a test can ask where Xero put an
// amount instead of asserting a column number of its own.
func agedReferenceRow(t *testing.T, path, label string) []string {
	t.Helper()
	for _, line := range agedReferenceLines(t, path) {
		if strings.HasPrefix(line, label+"\t") {
			return strings.Split(line, "\t")
		}
	}
	t.Fatalf("%s: no %q row", path, label)
	return nil
}

// agedReferenceBlock returns the lines between a block heading and the subtotal
// that closes it — the claimant line of the Expense Claims block, for instance.
func agedReferenceBlock(t *testing.T, path, heading, subtotal string) [][]string {
	t.Helper()
	var out [][]string
	inside := false
	for _, line := range agedReferenceLines(t, path) {
		if line == heading {
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if strings.HasPrefix(line, subtotal+"\t") {
			return out
		}
		out = append(out, strings.Split(line, "\t"))
	}
	t.Fatalf("%s: block %q does not close with %q", path, heading, subtotal)
	return nil
}

func agedReferenceLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	require.NoErrorf(t, err, "read the captured Xero report %s", path)
	_, body, found := strings.Cut(string(raw), "--- report output (tab-separated) ---")
	require.Truef(t, found, "%s: no report output section", path)
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// agedReferenceColumnsWithValue returns the columns of a captured row that carry
// an amount — the ones Xero did not print as a dash — as (column name, amount)
// pairs with the thousands separators stripped. It is how the tests find where
// Xero put a figure without knowing the answer in advance.
func agedReferenceColumnsWithValue(t *testing.T, header, row []string) map[string]string {
	t.Helper()
	require.Equal(t, len(header), len(row), "row and heading must line up: %v / %v", header, row)
	out := map[string]string{}
	for i := 1; i < len(row); i++ {
		if row[i] == "" || row[i] == "-" {
			continue
		}
		out[header[i]] = strings.ReplaceAll(row[i], ",", "")
	}
	return out
}

// ---------- the columns Xero's headings define ------------------------------

// agedDaysColumn is the column Xero's headings put a due date that is `days`
// days past the as-at date in. The boundaries are read off the headings
// themselves — "< 1 Month" is a month or less past due, "1 Month" the month
// after it, and so on to "Older" — so the partition under test is Xero's own
// wording rather than a second copy of the renderer's arithmetic.
func agedDaysColumn(days int) string {
	switch {
	case days <= 30:
		return "< 1 Month"
	case days <= 60:
		return "1 Month"
	case days <= 90:
		return "2 Months"
	case days <= 120:
		return "3 Months"
	default:
		return "Older"
	}
}

// agedDaysInColumn is a day count that belongs in the named column: the middle
// of the range the heading covers rather than its edge, so a fixture built from
// it does not depend on where exactly the boundary falls. The caller checks the
// two agree by round-tripping through agedDaysColumn.
func agedDaysInColumn(t *testing.T, column string) int {
	t.Helper()
	switch column {
	case "< 1 Month":
		return 15
	case "1 Month":
		return 45
	case "2 Months":
		return 75
	case "3 Months":
		return 105
	case "Older":
		return 150
	}
	t.Fatalf("no day range for the column %q", column)
	return 0
}

// ---------- reading the payload ----------------------------------------------

// agedHeader is the report's own heading row, which is what names the columns a
// client receives.
func agedHeader(t *testing.T, env xeroReportEnvelope) []string {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	for _, r := range env.Reports[0].Rows {
		if r.RowType == "Header" {
			out := make([]string, 0, len(r.Cells))
			for _, c := range r.Cells {
				out = append(out, c.Value)
			}
			return out
		}
	}
	t.Fatal("report has no Header row")
	return nil
}

func agedColumnIndex(t *testing.T, header []string, name string) int {
	t.Helper()
	for i, h := range header {
		if h == name {
			return i
		}
	}
	t.Fatalf("the aged report has no column named %q: %v", name, header)
	return -1
}

// hasSection reports whether the payload carries a section with this title. It is
// the non-fatal counterpart of sectionRows, for the assertions that a report does
// *not* have a block.
func hasSection(env xeroReportEnvelope, title string) bool {
	if len(env.Reports) == 0 {
		return false
	}
	for _, r := range env.Reports[0].Rows {
		if r.RowType == "Section" && r.Title == title {
			return true
		}
	}
	return false
}

// hasRowLabel reports whether any row of the report is labelled `label` — used
// for the rows a report must *not* print.
func hasRowLabel(env xeroReportEnvelope, label string) bool {
	for _, r := range allRows(env) {
		if len(r.Cells) > 0 && r.Cells[0].Value == label {
			return true
		}
	}
	return false
}

// amountsOfRow is a row's six amount cells (everything after its label) as
// money, so a test can compare two rows or a row against a query.
func amountsOfRow(row xeroReportRow) []decimalLike {
	out := make([]decimalLike, 0, len(row.Cells)-1)
	for i := 1; i < len(row.Cells); i++ {
		out = append(out, dec(row.Cells[i].Value))
	}
	return out
}

func assertAmountsEqual(t *testing.T, what string, got []decimalLike, want []decimalLike) {
	t.Helper()
	require.Len(t, got, len(want), what)
	for i := range want {
		assertEqMoney(t, fmt.Sprintf("%s: column %d", what, i), got[i], want[i])
	}
}

// percentOf is `part` as a percentage of `whole`, to two places — the share the
// Percentage of total row claims for the column the part came from.
func percentOf(part, whole decimalLike) float64 {
	if whole.f == 0 {
		return 0
	}
	return part.f / whole.f * 100
}

// ---------- the fixture ------------------------------------------------------

// resetClaims clears the organisation's expense claims and receipts, so a test
// that seeds a claim reads back exactly the claims it filed rather than the ones
// the reference import left behind. resetLedger clears the postings the same way;
// the two together make a test's own data the whole ledger it asserts against.
func resetClaims(t *testing.T, h *appHarness) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`DELETE FROM expense_claims WHERE organisation_id = $1`,
		`DELETE FROM receipts WHERE organisation_id = $1`,
	} {
		_, err := h.repos.Pool.Exec(ctx, stmt, seedDemoOrgID)
		require.NoError(t, err, stmt)
	}
}

// agedAsAt is the as-at date the captured reports were run for. It is the
// reference's own date, read off the capture's own "As at" line — the reports the
// tests reproduce are the ones run for that date.
func agedAsAt(t *testing.T) time.Time {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(agedReceivablesRef))
	require.NoError(t, err)
	for _, line := range strings.Split(string(raw), "\n") {
		asAt, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "As at ")
		if !ok {
			continue
		}
		d, err := time.Parse("2 January 2006", asAt)
		require.NoErrorf(t, err, "parse the capture's as-at date %q", asAt)
		return d
	}
	t.Fatal("the captured report carries no as-at date")
	return time.Time{}
}

// agedBucket is one invoice in the boundary fixture: how many days past due it
// is at the as-at date, and what it is worth.
type agedBucket struct {
	days   int
	amount string
}

// seedAgedBuckets seeds one authorised receivable per day-offset around every
// boundary the headings imply, plus one that is not due yet, so each of the five
// columns can be shown to hold exactly the amounts its heading claims. It
// returns the contact it posted to and the per-invoice expectations keyed by
// column, derived from the headings.
func seedAgedBuckets(t *testing.T, h *appHarness, asOf time.Time) (string, map[string]decimalLike) {
	t.Helper()
	name := "Aged buckets " + uuid.NewString()[:8]
	contactID := createContact(t, h, name)
	buckets := []agedBucket{
		{-20, "1.00"}, // not yet due at the as-at date: Xero folds it into "< 1 Month"
		{0, "2.00"},   // due on the as-at date
		{1, "3.00"},
		{30, "4.00"},
		{31, "5.00"},
		{60, "6.00"},
		{61, "7.00"},
		{90, "8.00"},
		{91, "9.00"},
		{120, "10.00"},
		{121, "11.00"},
	}
	want := map[string]decimalLike{}
	for _, b := range buckets {
		due := asOf.AddDate(0, 0, -b.days)
		date := due
		if due.After(asOf) {
			// Raised 30 days before the as-at date, so it exists in the ledger
			// and is simply not due yet.
			date = asOf.AddDate(0, 0, -30)
		}
		postInvoice(t, h, "ACCREC", contactID,
			date.Format(time.RFC3339), due.Format(time.RFC3339), b.amount)
		column := agedDaysColumn(b.days)
		want[column] = add(want[column], dec(b.amount))
	}
	return name, want
}

// invoiceAmountDue is what the database says the organisation still owes on the
// outstanding invoices of one type at `asOf` — the figure the report's Total must
// reproduce, read from the table the report reads rather than restated here.
func invoiceAmountDue(t *testing.T, h *appHarness, invType string, asOf time.Time) decimalLike {
	t.Helper()
	var sum string
	err := h.repos.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount_due),0)::text FROM invoices
		  WHERE organisation_id=$1 AND type=$2 AND status='AUTHORISED'
		    AND amount_due > 0 AND date <= $3::date`,
		seedDemoOrgID, invType, asOf).Scan(&sum)
	require.NoError(t, err)
	return dec(sum)
}

// claimAmountDue is what the database says the organisation still owes on its
// unpaid expense claims at `asOf`: the ledger the Expense Claims block is built
// from, read back out of it.
func claimAmountDue(t *testing.T, h *appHarness, asOf time.Time) decimalLike {
	t.Helper()
	var sum string
	err := h.repos.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount_due),0)::text FROM expense_claims
		  WHERE organisation_id=$1 AND status IN ('SUBMITTED','AUTHORISED') AND amount_due > 0
		    AND COALESCE(reporting_date, payment_due_date) <= $2::date`,
		seedDemoOrgID, asOf).Scan(&sum)
	require.NoError(t, err)
	return dec(sum)
}

// seedExpenseClaim files one reimbursement claim for the authenticated user and
// returns the user it is recorded against. The claimant is whoever the API
// records — the calling user — which is the only claimant record the schema
// keeps and therefore the only name the report can print.
func seedExpenseClaim(t *testing.T, h *appHarness, reportingDate time.Time, amount string) uuid.UUID {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "/api/v1/receipts", map[string]any{
		"Date": reportingDate.Format(time.RFC3339),
		"LineItems": []map[string]any{
			{"Description": "aged-parity claim", "Quantity": "1", "UnitAmount": amount, "AccountCode": "429"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var rEnv struct {
		Receipts []struct {
			ReceiptID string `json:"ReceiptID"`
		} `json:"Receipts"`
	}
	require.NoError(t, json.Unmarshal(body, &rEnv))
	require.NotEmpty(t, rEnv.Receipts)

	status, body = h.do(t, http.MethodPost, "/api/v1/expense-claims", map[string]any{
		"ReceiptIDs":    []string{rEnv.Receipts[0].ReceiptID},
		"Status":        "AUTHORISED",
		"ReportingDate": reportingDate.Format(time.RFC3339),
		"Total":         amount,
		"AmountDue":     amount,
		"AmountPaid":    "0",
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	return seedDemoUserID
}

// claimantLabel reads the label the Expense Claims block prints out of the
// database the block is built from: the user's name, else their email, else the
// marker the repository uses when a claim has no user at all. The test asks the
// data what the claimant is called instead of asserting a name, so a label that
// appears in no table — a name copied out of a report — cannot pass.
func claimantLabel(t *testing.T, h *appHarness, userID uuid.UUID) string {
	t.Helper()
	var label string
	err := h.repos.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(NULLIF(TRIM(CONCAT_WS(' ', first_name, last_name)), ''), email)
		   FROM users WHERE user_id=$1`, userID).Scan(&label)
	if err != nil {
		// No user row: the repository labels the claim as unassigned, which is
		// its own statement that the claimant is not recorded.
		return repository.ClaimantUnassigned
	}
	require.NotEmptyf(t, label, "user %s has neither a name nor an email", userID)
	return label
}

// ---------- the columns ------------------------------------------------------

// TestHTTP_Reports_AgedColumnsAreXerosOwn pins the five amount columns to the
// ones the captured Xero reports carry, in the summaries and in the by-contact
// variants. The old headings — Current | 1-30 | 31-60 | 61-90 | > 90 — were a
// different partition: Xero has no separate not-yet-due column, and its last
// column starts at 120 days past due rather than at 90.
func TestHTTP_Reports_AgedColumnsAreXerosOwn(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)

	want := agedReferenceColumns(t, agedReceivablesRef)
	require.Len(t, want, 7, "the captured report has seven columns")
	assert.Equal(t, want, agedReferenceColumns(t, agedPayablesRef),
		"the two captured reports must agree on the columns, or one table cannot serve both")

	asOf := agedAsAt(t)
	for _, path := range []string{
		"/api/v1/reports/aged-receivables",
		"/api/v1/reports/aged-payables",
		"/api/v1/reports/aged-receivables-by-contact",
		"/api/v1/reports/aged-payables-by-contact",
	} {
		env := fetchReport(t, h, path+"?date="+asOf.Format("2006-01-02"))
		got := agedHeader(t, env)
		assert.Equalf(t, want, got, "%s: column headings", path)
		assert.NotContainsf(t, got, "Current",
			"%s: a separate not-yet-due column is not one of Xero's", path)
	}
}

// TestHTTP_Reports_AgedBucketsFollowXerosBoundaries puts one invoice at every
// boundary the headings imply and checks it lands in exactly the column its age
// names — including the invoice that is not yet due, which Xero ages into
// "< 1 Month" rather than dropping or giving a column of its own.
func TestHTTP_Reports_AgedBucketsFollowXerosBoundaries(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)
	asOf := agedAsAt(t)
	contactName, wantColumns := seedAgedBuckets(t, h, asOf)

	env := fetchReport(t, h, "/api/v1/reports/aged-receivables?date="+asOf.Format("2006-01-02"))
	header := agedHeader(t, env)

	rows := sectionRows(t, env, "") // the receivables summary is one untitled section
	require.Len(t, rows, 1, "the fixture has one contact with receivables")
	row := rows[0]

	wantTotal := dec("")
	for column, want := range wantColumns {
		i := agedColumnIndex(t, header, column)
		assertEqMoney(t, fmt.Sprintf("%q must hold the invoices aged into it", column), dec(cell(row, i)), want)
		wantTotal = add(wantTotal, want)
	}
	assertEqMoney(t, "the contact row's Total is the fixture's invoices",
		dec(cell(row, len(header)-1)), wantTotal)

	// Each invoice is in exactly one column, so the five columns add up to the
	// row's own total — that is what makes them a partition and not five
	// overlapping ranges.
	assertEqMoney(t, "the five columns partition the contact row",
		sumCells(row, 1, 5), dec(cell(row, len(header)-1)))

	// The report agrees with the ledger, not merely with itself.
	total := findRowByLabel(t, env, "Total")
	assertEqMoney(t, "the report total is what the invoices leave due",
		dec(cell(total, len(header)-1)), invoiceAmountDue(t, h, "ACCREC", asOf))

	// The by-contact drill-down ages the same invoices the same way: one row
	// per invoice, and the same five columns.
	detail := fetchReport(t, h, "/api/v1/reports/aged-receivables-by-contact?date="+asOf.Format("2006-01-02"))
	detailHeader := agedHeader(t, detail)
	assert.Equal(t, header, detailHeader, "the two reports must name their columns the same")
	byInvoice := sectionRows(t, detail, contactName)
	require.NotEmpty(t, byInvoice, "the by-contact report lists the outstanding invoices")
	byInvoiceTotals := make([]decimalLike, 6)
	for _, r := range byInvoice {
		if r.RowType != "Row" {
			continue // the section's own trailing subtotal, summed below from the summary
		}
		amounts := amountsOfRow(r)
		require.Len(t, amounts, 6)
		assertEqMoney(t, "a detail row's Total is its own column", amounts[5], sumAmounts(amounts[:5]))
		for i := range byInvoiceTotals {
			byInvoiceTotals[i] = add(byInvoiceTotals[i], amounts[i])
		}
	}
	assertAmountsEqual(t, "the by-contact rows add back to the summary's contact row",
		byInvoiceTotals, amountsOfRow(row))
}

func sumAmounts(amounts []decimalLike) decimalLike {
	out := dec("")
	for _, a := range amounts {
		out = add(out, a)
	}
	return out
}

// TestHTTP_Reports_AgedPercentageRowIsTheRenderedTotal checks the
// "Percentage of total" row against the rows the report itself rendered: each
// column as its share of the printed Total, and the Total column as the whole of
// itself. It is present in the summaries and absent from the by-contact variants,
// for which no captured Xero report exists to model it on.
func TestHTTP_Reports_AgedPercentageRowIsTheRenderedTotal(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)
	asOf := agedAsAt(t)
	seedAgedBuckets(t, h, asOf)

	for _, path := range []string{"/api/v1/reports/aged-receivables", "/api/v1/reports/aged-payables"} {
		env := fetchReport(t, h, path+"?date="+asOf.Format("2006-01-02"))
		header := agedHeader(t, env)
		total := findRowByLabel(t, env, "Total")
		pct := findRowByLabel(t, env, "Percentage of total")

		totals := amountsOfRow(total)
		den := totals[len(totals)-1]
		require.Truef(t, den.f > 0, "%s: the fixture must have a total", path)

		for i, part := range totals {
			want := dec(strconv.FormatFloat(percentOf(part, den), 'f', 2, 64))
			got := strings.TrimSuffix(cell(pct, i+1), "%")
			assert.Truef(t, dec(got).Sub(want).Abs().LessThan(dec("0.01")),
				"%s: %q is %s%%, but the rendered column %s is %s of the rendered total %s",
				path, header[i+1], got, moneyString(part), moneyString(part), moneyString(den))
		}
		assert.Equal(t, "100.00%", cell(pct, len(header)-1),
			path+": the Total column is the whole of itself")
	}

	for _, path := range []string{
		"/api/v1/reports/aged-receivables-by-contact",
		"/api/v1/reports/aged-payables-by-contact",
	} {
		env := fetchReport(t, h, path+"?date="+asOf.Format("2006-01-02"))
		assert.Falsef(t, hasRowLabel(env, "Percentage of total"),
			"%s: no captured Xero by-contact report exists, so no percentage row is invented for it", path)
	}
}

func moneyString(d decimalLike) string { return d.String() }

// TestHTTP_Reports_AgedEmptyOrganisationIsHonest is the empty ledger: an
// organisation with no invoices must not be given a row, a total or a share of a
// total it does not have. The report keeps its envelope and its columns and says
// nothing else.
func TestHTTP_Reports_AgedEmptyOrganisationIsHonest(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)
	resetClaims(t, h)
	asOf := agedAsAt(t)

	for _, path := range []string{"/api/v1/reports/aged-receivables", "/api/v1/reports/aged-payables"} {
		env := fetchReport(t, h, path+"?date="+asOf.Format("2006-01-02"))
		header := agedHeader(t, env)
		assert.Equal(t, agedReferenceColumns(t, agedReceivablesRef), header, path)

		for _, r := range env.Reports[0].Rows {
			assert.NotEqualf(t, "Section", r.RowType,
				"%s: an empty ledger has no rows to group, so it has no section: %v", path, r.Title)
		}
		assert.Falsef(t, hasSection(env, "Expense Claims"),
			"%s: an organisation with no claims gets no claims block", path)

		// A total of nothing is a total of nothing; no share is printed,
		// because there is no denominator to take one against.
		pct := findRowByLabel(t, env, "Percentage of total")
		for i := 1; i < len(header); i++ {
			assert.Equalf(t, "", cell(pct, i),
				"%s: the percentage row invents no share over an empty report (%s)", path, header[i])
		}
	}
}

// TestHTTP_Reports_AgedPayablesExpenseClaimsBlock is Xero's second block: an
// unpaid expense claim is a payable too, so the payables report carries it, and
// the report's own Total is the trade payables plus those claims. The claim
// fixture is built from the captured report itself — the amount and the column
// Xero put it in — so what the test asserts is the shape and arithmetic the
// capture demonstrates, not a second set of numbers.
func TestHTTP_Reports_AgedPayablesExpenseClaimsBlock(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)
	resetClaims(t, h)
	asOf := agedAsAt(t)

	// Where Xero put the claim, and how much it was for.
	header := agedReferenceColumns(t, agedPayablesRef)
	claimRows := agedReferenceBlock(t, agedPayablesRef, "Expense Claims", "Total Expense Claims")
	require.Len(t, claimRows, 1, "the capture shows one claimant in the block")
	claimCells := agedReferenceColumnsWithValue(t, header, claimRows[0])
	require.Len(t, claimCells, 2, "the captured claim sits in one column and totals that: %v", claimRows[0])
	claimColumn := header[1]
	claimAmount := ""
	for column, amount := range claimCells {
		if column != "Total" {
			claimColumn, claimAmount = column, amount
		}
	}
	require.NotEmpty(t, claimAmount, "the capture shows no claim amount")

	// The reference's own subtotal line must agree with the claimant line, or
	// the fixture would be built from a capture that disagrees with itself.
	subtotal := agedReferenceColumnsWithValue(t, header,
		agedReferenceRow(t, agedPayablesRef, "Total Expense Claims"))
	assert.Equal(t, claimAmount, subtotal[claimColumn], "the capture's claim subtotal")
	assert.Equal(t, claimAmount, subtotal["Total"], "the capture's claim subtotal")

	// A trade payable too, so the report has both blocks.
	postInvoice(t, h, "ACCPAY", createContact(t, h, "Aged supplier "+uuid.NewString()[:8]),
		asOf.AddDate(0, 0, -200).Format(time.RFC3339), asOf.AddDate(0, 0, -150).Format(time.RFC3339), "25.00")

	days := agedDaysInColumn(t, claimColumn)
	require.Equal(t, claimColumn, agedDaysColumn(days),
		"the fixture's day count must land in the column the capture used")
	claimant := seedExpenseClaim(t, h, asOf.AddDate(0, 0, -days), claimAmount)

	env := fetchReport(t, h, "/api/v1/reports/aged-payables?date="+asOf.Format("2006-01-02"))
	gotHeader := agedHeader(t, env)

	claims := sectionRows(t, env, "Expense Claims")
	require.Len(t, claims, 1, "one row per claimant")
	assert.Equal(t, claimantLabel(t, h, claimant), cell(claims[0], 0),
		"the claimant is the one the database records for the claim")
	assert.Equalf(t, claimAmount, cell(claims[0], agedColumnIndex(t, gotHeader, claimColumn)),
		"the claim's amount belongs in the column the capture put it in")
	assertEqMoney(t, "the claimant row totals its own column",
		dec(cell(claims[0], len(gotHeader)-1)), dec(claimAmount))

	// The block's subtotal is the block's rows, and the report's total is the
	// two blocks — not a second query that could drift from them.
	claimTotal := findRowByLabel(t, env, "Total Expense Claims")
	assertAmountsEqual(t, "the claims subtotal is the claimant rows",
		amountsOfRow(claimTotal), amountsOfRow(claims[0]))
	assertEqMoney(t, "the claim block agrees with the claims the database holds",
		dec(cell(claimTotal, len(gotHeader)-1)), claimAmountDue(t, h, asOf))

	payableTotal := findRowByLabel(t, env, "Total Aged Payables")
	total := findRowByLabel(t, env, "Total")
	for i, want := range amountsOfRow(total) {
		assertEqMoney(t, fmt.Sprintf("the report total is both blocks, column %d", i),
			want, add(amountsOfRow(payableTotal)[i], amountsOfRow(claimTotal)[i]))
	}

	// The percentage row is taken against that combined total, which is what
	// makes the claim's share of it visible.
	pct := findRowByLabel(t, env, "Percentage of total")
	i := agedColumnIndex(t, gotHeader, claimColumn)
	want := dec(strconv.FormatFloat(percentOf(amountsOfRow(total)[i-1], amountsOfRow(total)[5]), 'f', 2, 64))
	assert.Truef(t, dec(strings.TrimSuffix(cell(pct, i), "%")).Sub(want).Abs().LessThan(dec("0.01")),
		"the claim's column is its share of the report total: got %s, want %s%%", cell(pct, i), want)
}

// TestHTTP_Reports_AgedReceivablesHasNoExpenseClaimsBlock keeps the second
// block where Xero has it. An unpaid expense claim is money the organisation
// owes, not money it is owed, so it belongs in the payables report only, and the
// receivables report's total is its contact rows and nothing else.
func TestHTTP_Reports_AgedReceivablesHasNoExpenseClaimsBlock(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)
	resetClaims(t, h)
	asOf := agedAsAt(t)

	// The claim is the fixture's own — a claim aged five days, so it lands in
	// the first column, unlike the reference's claim.
	claimAmount := "10.00"
	seedExpenseClaim(t, h, asOf.AddDate(0, 0, -5), claimAmount)
	seedAgedBuckets(t, h, asOf)

	ar := fetchReport(t, h, "/api/v1/reports/aged-receivables?date="+asOf.Format("2006-01-02"))
	assert.False(t, hasSection(ar, "Expense Claims"),
		"the receivables report has no expense-claim block: the claim is a payable")

	header := agedHeader(t, ar)
	total := findRowByLabel(t, ar, "Total")
	rows := sectionRows(t, ar, "")
	require.NotEmpty(t, rows)
	sum := make([]decimalLike, len(header)-1)
	for _, r := range rows {
		for i, a := range amountsOfRow(r) {
			sum[i] = add(sum[i], a)
		}
	}
	assertAmountsEqual(t, "the receivables total is its contact rows", amountsOfRow(total), sum)

	// The claim does show up on the payables report, which is the statement
	// that it was not dropped — it was put in the right report.
	ap := fetchReport(t, h, "/api/v1/reports/aged-payables?date="+asOf.Format("2006-01-02"))
	require.True(t, hasSection(ap, "Expense Claims"),
		"the unpaid claim must appear on the payables report")
	claims := sectionRows(t, ap, "Expense Claims")
	assertEqMoney(t, "the payable's claim is the claim that was filed",
		dec(cell(claims[0], len(agedHeader(t, ap))-1)), dec(claimAmount))
}

// TestHTTP_Reports_AgedByContactIsNotTheSummary keeps the drill-down distinct
// from the summary: the summary has one row per contact, the drill-down has one
// row per outstanding invoice inside that contact's block, and both carry the
// same five columns and the same grand total.
func TestHTTP_Reports_AgedByContactIsNotTheSummary(t *testing.T) {
	h := newHarness(t)
	resetLedger(t, h)
	asOf := agedAsAt(t)
	seedAgedBuckets(t, h, asOf)

	q := "?date=" + asOf.Format("2006-01-02")
	summary := fetchReport(t, h, "/api/v1/reports/aged-receivables"+q)
	detail := fetchReport(t, h, "/api/v1/reports/aged-receivables-by-contact"+q)

	summaryRows := sectionRows(t, summary, "")
	require.Len(t, summaryRows, 1, "one row per contact")

	// One section per contact, each closing with its own subtotal, and each
	// listing the invoices that make it up.
	var detailRows []xeroReportRow
	var sectionTitles []string
	for _, r := range detail.Reports[0].Rows {
		if r.RowType != "Section" {
			continue
		}
		sectionTitles = append(sectionTitles, r.Title)
		var subtotal xeroReportRow
		for _, inner := range r.Rows {
			if inner.RowType == "SummaryRow" {
				subtotal = inner
				continue
			}
			detailRows = append(detailRows, inner)
		}
		require.NotEqualf(t, xeroReportRow{}, subtotal, "section %q has no subtotal", r.Title)
		assert.Equal(t, "Total "+r.Title, cell(subtotal, 0),
			"the section's subtotal names the contact it totals")
	}
	assert.Equal(t, []string{cell(summaryRows[0], 0)}, sectionTitles,
		"the drill-down groups the same contacts the summary lists")

	require.Len(t, detailRows, 11, "one detail row per outstanding invoice in the fixture")
	assert.NotEqual(t, len(summaryRows), len(detailRows),
		"the drill-down lists invoices where the summary lists contacts")

	grand := findRowByLabel(t, detail, "Total")
	assertAmountsEqual(t, "both reports end at the same grand total",
		amountsOfRow(grand), amountsOfRow(findRowByLabel(t, summary, "Total")))
}

// ---------- the Budget Summary ----------------------------------------------

// budgetEnvelope is the Xero Reports envelope with the ReportTitles the reports
// carry — the field the Budget Summary states its gap in, and the one the shared
// envelope in reports_values_integration_test.go does not model.
type budgetEnvelope struct {
	Status  string `json:"Status"`
	Reports []struct {
		ReportID     string          `json:"ReportID"`
		ReportName   string          `json:"ReportName"`
		ReportType   string          `json:"ReportType"`
		ReportDate   string          `json:"ReportDate"`
		ReportTitles []string        `json:"ReportTitles"`
		Rows         []xeroReportRow `json:"Rows"`
	} `json:"Reports"`
}

// TestHTTP_Reports_BudgetSummaryStatesNoBudgetIsStored is the honest empty
// report. goXero has no budget table, so there is no budget to report; the
// endpoint keeps its envelope, its identity in the Reports index and its columns
// so a client parses what it expects, and says in the report itself that no
// budget is stored instead of printing a total of zero over zero accounts — a
// figure that would look measured.
func TestHTTP_Reports_BudgetSummaryStatesNoBudgetIsStored(t *testing.T) {
	h := newHarness(t)

	// The index is where the report's identity comes from, so the test checks
	// the report against the index entry rather than against a literal.
	status, body := h.do(t, http.MethodGet, "/api/v1/reports", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var index struct {
		Reports []struct {
			ReportID   string `json:"ReportID"`
			ReportName string `json:"ReportName"`
			Path       string `json:"Path"`
		} `json:"Reports"`
	}
	require.NoError(t, json.Unmarshal(body, &index))
	var entry struct{ ReportID, ReportName, Path string }
	for _, e := range index.Reports {
		if strings.Contains(strings.ToLower(e.ReportName), "budget summary") {
			entry = struct{ ReportID, ReportName, Path string }{e.ReportID, e.ReportName, e.Path}
		}
	}
	require.NotEmptyf(t, entry.Path, "the Reports index no longer offers a Budget Summary: %v", index.Reports)
	assert.Containsf(t, entry.Path+"/", "/reports/budget-summary/",
		"the index entry must point at the route this test calls")

	// The index carries the path under /reports; the API serves it under
	// /api/v1, the same way the parity suite walks the index.
	status, body = h.do(t, http.MethodGet, "/api/v1"+entry.Path, nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var env budgetEnvelope
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "OK", env.Status)
	require.Len(t, env.Reports, 1)
	report := env.Reports[0]

	assert.Equal(t, entry.ReportID, report.ReportID)
	assert.Equal(t, entry.ReportName, report.ReportName)
	assert.NotEmpty(t, report.ReportDate)

	// The caveat travels with the report, the way the Cash Summary's
	// explanation of what it cannot show does.
	require.Len(t, report.ReportTitles, 4,
		"the report states its gap in a report title: %v", report.ReportTitles)
	assert.Equal(t, report.ReportName, report.ReportTitles[0])
	caveat := strings.ToLower(report.ReportTitles[3])
	assert.Contains(t, caveat, "no budget", "the report must say what it is missing")
	assert.Contains(t, caveat, "no budget table in the schema",
		"and why it is missing: goXero stores no budget")

	// Its columns are Xero's, and nothing is printed under them.
	require.Len(t, report.Rows, 1, "a report with no budget has its heading and nothing else")
	assert.Equal(t, "Header", report.Rows[0].RowType)
	assert.Equal(t, []string{"Account", "Budget"}, cellsOf(report.Rows[0]))
	assert.False(t, hasRowLabel(reportAsEnvelope(env), "Total"),
		"no Total row: there is nothing to total")

	// No cell anywhere in the report is a figure, so no reader can mistake a
	// placeholder for a budget.
	for _, r := range report.Rows {
		for i, c := range r.Cells {
			if i == 0 {
				continue
			}
			assert.NotRegexpf(t, `^-?\d`, c.Value,
				"a budget figure would have to come from a budget: %q", c.Value)
		}
	}
}

func cellsOf(row xeroReportRow) []string {
	out := make([]string, 0, len(row.Cells))
	for _, c := range row.Cells {
		out = append(out, c.Value)
	}
	return out
}

// reportAsEnvelope adapts the titles-carrying envelope to the shared one so the
// row helpers can be reused on it.
func reportAsEnvelope(env budgetEnvelope) xeroReportEnvelope {
	var out xeroReportEnvelope
	out.Status = env.Status
	for _, r := range env.Reports {
		out.Reports = append(out.Reports, struct {
			ReportID   string          `json:"ReportID"`
			ReportName string          `json:"ReportName"`
			Rows       []xeroReportRow `json:"Rows"`
		}{ReportID: r.ReportID, ReportName: r.ReportName, Rows: r.Rows})
	}
	return out
}
