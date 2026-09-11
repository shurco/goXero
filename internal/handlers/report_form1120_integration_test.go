package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/repository"
)

// form1120UnmappedTitle is the heading of the section listing the accounts no
// 1120 line claims. The acceptance criteria are stated in terms of it, so it is
// pinned here by name.
const form1120UnmappedTitle = "Accounts with no 1120 line"

// TestHTTP_Form1120_Workpaper is the acceptance test for the Form 1120
// workpaper. It drives the seeded demo organisation — the same tenant every
// other report test uses, carrying the chart Xero's reference capture seeded —
// through two events in one tax year:
//
//   - a $100 sale on account, which lands on revenue account 200 (Sales, the
//     seeded chart's gross receipts account) and account 610 (Accounts
//     Receivable);
//   - a $100 manual journal spending through account 420 (Entertainment), which
//     the mapping table carries on line 26 "Other deductions" — Page 1 names no
//     line for it.
//
// It then asserts the three things the workpaper has to get right: a form line
// carries a real figure, every account that carries a figure anywhere in the
// books is placed on a line, and the whole thing comes back wrapped in the
// canonical Xero Reports envelope.
//
// The window stops at 30 June. The seeded books carry the captured Xero bank
// transactions, all of them dated August and September 2026, and the workpaper
// has to be measured on the test's own two events rather than on the capture's
// — so the period is the half of the tax year before the capture starts, and
// the figures asserted below are those two events and nothing else. The
// ledger oracle reads the same window for the same reason.
func TestHTTP_Form1120_Workpaper(t *testing.T) {
	h := newHarness(t)

	contactID := createContact(t, h, "Form 1120 "+uuid.NewString()[:6])
	createForm1120Invoice(t, h, contactID, "2026-03-01T00:00:00Z", "100")
	postForm1120Journal(t, h, "2026-04-01T00:00:00Z", "420", "090", "100")

	// The window the workpaper is read over is also the window the ledger
	// oracle is read over, so the two cannot drift apart.
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	window := fmt.Sprintf("fromDate=%s&toDate=%s", from.Format(time.DateOnly), to.Format(time.DateOnly))
	env := fetchForm1120(t, h, "/api/v1/reports/form-1120?"+window)

	// ── The envelope ───────────────────────────────────────────────────────
	require.Len(t, env.Reports, 1)
	report := env.Reports[0]
	assert.Equal(t, "Form1120", report.ReportID)
	assert.Equal(t, "Form 1120 Workpaper", report.ReportName)
	require.Len(t, report.ReportTitles, 3)
	assert.Equal(t, "Demo Company (Global)", report.ReportTitles[1])
	assert.Contains(t, report.ReportTitles[2], "1 January 2026")
	assert.NotEmpty(t, report.ReportDate)
	assert.NotEmpty(t, report.Rows)

	// ── (a) at least one 1120 line carries a figure ────────────────────────
	assert.Equal(t, "100.00", form1120LineValue(t, env, "1a Gross receipts"),
		"the sale must land on gross receipts")
	assert.Equal(t, "100.00", form1120LineValue(t, env, "3 Gross profit"))
	assert.Equal(t, "100.00", form1120LineValue(t, env, "11 Total income"),
		"total income must roll the sub-totals up")
	// Entertainment has no line of its own on Page 1, so the journal lands on
	// line 26 and rides up into total deductions — the whole of the $100, at the
	// book figure the account carries.
	assert.Equal(t, "100.00", form1120LineValue(t, env, "26 Other deductions"),
		"the entertainment spend must reach Other deductions")
	assert.Equal(t, "100.00", form1120LineValue(t, env, "27 Total deductions"))
	assert.Equal(t, "0.00", form1120LineValue(t, env, "30 Taxable income"))
	assert.NotEqual(t, "0.00", form1120LineValue(t, env, "11 Total income"),
		"at least one 1120 line must carry a figure")

	// Line 31 is deliberately blank: the report states the figure the rate
	// applies to instead of inventing a tax rate.
	assert.Equal(t, "0.00", form1120LineValue(t, env, "31 Total tax (apply the current rate)"))
	assert.True(t, form1120LineHasNote(env, "31 Total tax (apply the current rate)"),
		"line 31 must carry the note that leaves the rate to the accountant")

	// ── (b) every account that carries a figure is placed ──────────────────
	//
	// The oracle is the General Ledger Detail report, which walks the whole
	// chart of accounts and reports each account's opening balance, movement
	// and closing balance — the same three facts, but through a different
	// report than the one under test.
	accounts := accountIDsByCode(t, h)
	carries := accountsCarryingFigures(t, h, from, to)
	require.Contains(t, carries, accounts["200"].String(), "the sale must reach the books")
	require.Contains(t, carries, accounts["420"].String(), "the journal must reach the books")

	placement := form1120Placements(env)
	for id, code := range carries {
		_, onLine := placement.onLine[id]
		assert.Truef(t, onLine || placement.unmapped[id],
			"account %s carries a figure but the workpaper places it nowhere", code)
	}
	for id, times := range placement.seen {
		assert.Equalf(t, 1, times, "account %s is printed %d times in one workpaper", id, times)
	}
	for id := range placement.unmapped {
		assert.Containsf(t, carries, id,
			"the %q section lists account %s, which carries no figure", form1120UnmappedTitle, id)
	}
	for id := range placement.onLine {
		assert.Containsf(t, carries, id,
			"line %s lists account %s, which carries no figure", placement.onLine[id], id)
	}

	// The mapping table's three answers, by name: a named line, a type-wide
	// rule, and an expense the form has no line for.
	assert.Equal(t, "1a Gross receipts", placement.onLine[accounts["200"].String()])
	assert.Equal(t, "L1 Cash", placement.onLine[accounts["090"].String()])
	assert.Equal(t, "L2 Trade notes and accounts receivable", placement.onLine[accounts["610"].String()])
	assert.Equal(t, "26 Other deductions", placement.onLine[accounts["420"].String()],
		"Entertainment has no line of its own and belongs in Other deductions")

	// The section that says the workpaper is unfinished still renders — it is
	// the report's own check — and over this window it has nothing to report.
	require.True(t, form1120HasSection(env, form1120UnmappedTitle))
	assert.True(t, form1120SectionHasHeader(env, form1120UnmappedTitle))
	assert.Empty(t, form1120SectionAccounts(t, env, form1120UnmappedTitle),
		"every account in the seeded chart reaches a line")

	// ── Schedule L is a balance sheet and has to balance ───────────────────
	assert.Equal(t, "0.00", form1120LineValue(t, env, "LCHECK Difference (line 15 less line 27)"),
		"total assets must equal total liabilities and shareholders' equity")

	// ── Schedule M-1 opens with the book result ────────────────────────────
	assert.Equal(t, "0.00", form1120LineValue(t, env, "M1 Net income per books"),
		"book income is the sale less the entertainment expense")

	// ── The Xero-compatible path answers the same report ───────────────────
	xro := fetchForm1120(t, h, "/api.xro/2.0/Reports/Form1120?"+window)
	require.Len(t, xro.Reports, 1)
	assert.Equal(t, "Form1120", xro.Reports[0].ReportID)
	assert.Equal(t, "100.00", form1120LineValue(t, xro, "1a Gross receipts"))

	// Bad input is the caller's error, not a 500.
	status, _ := h.do(t, http.MethodGet, "/api/v1/reports/form-1120?fromDate=2026-12-31&toDate=2026-01-01", nil, true)
	assert.Equal(t, http.StatusBadRequest, status)
	status, _ = h.do(t, http.MethodGet, "/api/v1/reports/form-1120?toDate=not-a-date", nil, true)
	assert.Equal(t, http.StatusBadRequest, status)
}

// TestHTTP_Form1120_EverySeededAccountReachesALine is the workpaper's own
// check, read from the outside: over the organisation's tax year, no account in
// the chart the migration seeds reaches no 1120 line.
//
// It is deliberately read over the whole tax year rather than the half the
// acceptance test uses. The capture the chart seeds is dated August and
// September 2026, and those are the postings that put figures on the accounts
// Page 1 does not name — so a window that stops at 30 June cannot see them, and
// a mapping table that leaves them out would pass.
//
// The section is asserted to render as well as to be empty: an empty section
// that has stopped being printed would be a workpaper that no longer checks
// itself. The oracle for which accounts carry a figure is the General Ledger
// Detail report, not the workpaper's own queries.
func TestHTTP_Form1120_EverySeededAccountReachesALine(t *testing.T) {
	h := newHarness(t)

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	window := fmt.Sprintf("fromDate=%s&toDate=%s", from.Format(time.DateOnly), to.Format(time.DateOnly))
	env := fetchForm1120(t, h, "/api.xro/2.0/Reports/Form1120?"+window)

	// The section still renders. "Empty" has to mean "nothing to report", not
	// "no longer printed".
	require.True(t, form1120HasSection(env, form1120UnmappedTitle),
		"the workpaper must still print its %q check", form1120UnmappedTitle)
	assert.True(t, form1120SectionHasHeader(env, form1120UnmappedTitle),
		"the %q section must still carry its column headings", form1120UnmappedTitle)
	assert.True(t, form1120LineHasNote(env, form1120UnmappedTitle))

	// The seeded books do carry expenses — the accounts below are the ones that
	// used to reach no line, and between them they are the whole of the gap.
	accounts := accountIDsByCode(t, h)
	carries := accountsCarryingFigures(t, h, from, to)
	require.NotEmpty(t, carries, "the seeded books must carry figures for this check to mean anything")
	for _, code := range []string{"408", "420", "445", "449", "453", "461", "489", "493"} {
		require.Containsf(t, accounts, code, "the seeded chart must carry account %s", code)
		require.Containsf(t, carries, accounts[code].String(),
			"account %s must carry a figure in the seeded books", code)
	}

	// Every account with a figure is on a line, and none is on two.
	placement := form1120Placements(env)
	for id, code := range carries {
		_, onLine := placement.onLine[id]
		assert.Truef(t, onLine, "account %s carries a figure but reaches no 1120 line", code)
	}
	for id, times := range placement.seen {
		assert.Equalf(t, 1, times, "account %s is printed %d times in one workpaper", id, times)
	}
	assert.Emptyf(t, placement.unmapped,
		"the %q section must be empty for the chart the migration seeds", form1120UnmappedTitle)
}

// TestHTTP_Form1120_DefaultsToFinancialYear checks that with no dates the
// workpaper covers the organisation's financial year, the way the other reports
// default their window.
func TestHTTP_Form1120_DefaultsToFinancialYear(t *testing.T) {
	h := newHarness(t)
	env := fetchForm1120(t, h, "/api/v1/reports/form-1120")
	require.Len(t, env.Reports, 1)
	require.Len(t, env.Reports[0].ReportTitles, 3)
	require.NotEmpty(t, env.Reports[0].Rows)
	// The demo organisation has a 31 December year end, so the title block has
	// to name the financial year the workpaper covered.
	assert.Contains(t, env.Reports[0].ReportTitles[2], "1 January")
}

// createForm1120Invoice authorises an invoice dated `date` for `contactID`,
// with its single line on account 200 — Sales in the seeded chart, and the
// account the workpaper has to place on gross receipts. The shared helper posts
// its line to 400, which in the seeded chart is Advertising.
func createForm1120Invoice(t *testing.T, h *appHarness, contactID, date, amount string) {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "/api/v1/invoices", map[string]any{
		"Type":            "ACCREC",
		"Status":          "AUTHORISED",
		"ContactID":       contactID,
		"Date":            date,
		"DueDate":         date,
		"LineAmountTypes": "Exclusive",
		"LineItems": []map[string]any{
			{"Description": "Work", "Quantity": "1", "UnitAmount": amount, "AccountCode": "200", "TaxAmount": "0"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
}

// postForm1120Journal posts a two-line journal that debits `debitCode` and credits
// `creditCode`, putting real activity on both accounts.
func postForm1120Journal(t *testing.T, h *appHarness, date, debitCode, creditCode, amount string) {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "/api/v1/manual-journals", map[string]any{
		"Narration": "Form 1120 workpaper test",
		"Status":    "POSTED",
		"Date":      date,
		"JournalLines": []map[string]any{
			{"AccountCode": debitCode, "LineAmount": amount},
			{"AccountCode": creditCode, "LineAmount": "-" + amount},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
}

// accountIDsByCode returns the demo organisation's chart of accounts keyed by
// account code.
func accountIDsByCode(t *testing.T, h *appHarness) map[string]uuid.UUID {
	t.Helper()
	status, body := h.do(t, http.MethodGet, "/api/v1/accounts", nil, true)
	require.Equal(t, http.StatusOK, status, string(body))
	var env struct {
		Payload struct {
			Accounts []struct {
				AccountID string `json:"AccountID"`
				Code      string `json:"Code"`
			} `json:"Accounts"`
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.NotEmpty(t, env.Payload.Accounts)
	out := make(map[string]uuid.UUID, len(env.Payload.Accounts))
	for _, a := range env.Payload.Accounts {
		out[a.Code] = uuid.MustParse(a.AccountID)
	}
	return out
}

// accountsCarryingFigures returns the AccountIDs of every account the books
// give a figure to over [from, to], keyed to its code for readable failures. It
// reads the General Ledger Detail report rather than the workpaper's own
// queries, so the check is independent of the code under test — and it is given
// the same window as the workpaper, or it would hold the workpaper to accounts
// the period it covers does not include.
func accountsCarryingFigures(t *testing.T, h *appHarness, from, to time.Time) map[string]string {
	t.Helper()
	groups, err := h.repos.Reports.GeneralLedgerDetail(context.Background(), seedDemoOrgID, from, to)
	require.NoError(t, err)
	out := map[string]string{}
	for _, g := range groups {
		if g.Opening.IsZero() && g.Debit.IsZero() && g.Credit.IsZero() && g.Closing.IsZero() {
			continue
		}
		out[g.AccountID.String()] = g.AccountCode
	}
	return out
}

// ── Reading the Xero envelope ───────────────────────────────────────────────

type form1120Envelope struct {
	Id           string `json:"Id"`
	Status       string `json:"Status"`
	ProviderName string `json:"ProviderName"`
	DateTimeUTC  time.Time
	Reports      []struct {
		ReportID     string        `json:"ReportID"`
		ReportName   string        `json:"ReportName"`
		ReportType   string        `json:"ReportType"`
		ReportTitles []string      `json:"ReportTitles"`
		ReportDate   string        `json:"ReportDate"`
		Rows         []form1120Row `json:"Rows"`
	} `json:"Reports"`
}

type form1120Row struct {
	RowType string         `json:"RowType"`
	Title   string         `json:"Title"`
	Cells   []form1120Cell `json:"Cells"`
	Rows    []form1120Row  `json:"Rows"`
}

type form1120Cell struct {
	Value      string             `json:"Value"`
	Attributes []form1120CellAttr `json:"Attributes"`
}

type form1120CellAttr struct {
	ID    string `json:"Id"`
	Value string `json:"Value"`
}

// fetchForm1120 runs a report endpoint and asserts the envelope is the one
// every goXero report answers with.
func fetchForm1120(t *testing.T, h *appHarness, path string) form1120Envelope {
	t.Helper()
	status, body := h.do(t, http.MethodGet, path, nil, true)
	require.Equalf(t, http.StatusOK, status, "%s: %s", path, string(body))
	var env form1120Envelope
	require.NoError(t, json.Unmarshal(body, &env))
	assert.Equal(t, "OK", env.Status, path)
	assert.Equal(t, "goxero", env.ProviderName, path)
	assert.NotEmpty(t, env.Id, path)
	assert.False(t, env.DateTimeUTC.IsZero(), path)
	return env
}

// walkForm1120 visits every row of the report in document order. `trail` is the
// chain of section titles the row sits under, outermost first.
func walkForm1120(rows []form1120Row, trail []string, visit func(row form1120Row, trail []string)) {
	for _, row := range rows {
		next := trail
		if row.RowType == "Section" && row.Title != "" {
			next = append(append([]string{}, trail...), row.Title)
		}
		visit(row, next)
		walkForm1120(row.Rows, next, visit)
	}
}

// form1120LineValue returns the figure a named line prints — the second cell of
// the summary row inside that line's own section.
func form1120LineValue(t *testing.T, env form1120Envelope, title string) string {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	out, found := "", false
	walkForm1120(env.Reports[0].Rows, nil, func(row form1120Row, trail []string) {
		if found || row.RowType != "SummaryRow" || len(row.Cells) < 2 {
			return
		}
		if len(trail) == 0 || trail[len(trail)-1] != title {
			return
		}
		out, found = row.Cells[1].Value, true
	})
	require.Truef(t, found, "line %q not found in the workpaper", title)
	return out
}

// form1120LineHasNote reports whether a line carries an explanatory row, which
// is how the workpaper says something the form's own wording does not.
func form1120LineHasNote(env form1120Envelope, title string) bool {
	if len(env.Reports) == 0 {
		return false
	}
	found := false
	walkForm1120(env.Reports[0].Rows, nil, func(row form1120Row, trail []string) {
		if found || len(row.Cells) != 1 || row.Cells[0].Value == "" {
			return
		}
		if len(trail) == 0 || trail[len(trail)-1] != title {
			return
		}
		found = true
	})
	return found
}

// form1120HasSection reports whether the report prints a named section at all.
func form1120HasSection(env form1120Envelope, title string) bool {
	return form1120HasSectionRow(env, title, func(form1120Row) bool { return true })
}

// form1120SectionHasHeader reports whether a named section still prints its
// column-heading row — the "Account | Type | Period amount | Closing balance"
// line a reader needs before the rows under it mean anything.
func form1120SectionHasHeader(env form1120Envelope, title string) bool {
	return form1120HasSectionRow(env, title, func(row form1120Row) bool {
		return row.RowType == "Header" && len(row.Cells) > 1
	})
}

// form1120HasSectionRow reports whether a named section holds a row the
// predicate accepts. The section itself counts, so a section that prints with
// nothing under it is still found.
func form1120HasSectionRow(env form1120Envelope, title string, want func(form1120Row) bool) bool {
	if len(env.Reports) == 0 {
		return false
	}
	found := false
	walkForm1120(env.Reports[0].Rows, nil, func(row form1120Row, trail []string) {
		if found {
			return
		}
		for _, t := range trail {
			if t == title && want(row) {
				found = true
				return
			}
		}
	})
	return found
}

// form1120SectionAccounts returns the AccountIDs of the account rows inside a
// named top-level section.
func form1120SectionAccounts(t *testing.T, env form1120Envelope, title string) []string {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	var out []string
	walkForm1120(env.Reports[0].Rows, nil, func(row form1120Row, trail []string) {
		if len(trail) == 0 || trail[0] != title || len(row.Cells) == 0 {
			return
		}
		if id := form1120CellAccount(row.Cells[0]); id != "" {
			out = append(out, id)
		}
	})
	return out
}

// form1120CellAccount reads the `account` attribute the renderers put on a cell
// so a figure can be traced back to the chart of accounts.
func form1120CellAccount(cell form1120Cell) string {
	for _, a := range cell.Attributes {
		if a.ID == "account" {
			return a.Value
		}
	}
	return ""
}

// form1120Placement is where the workpaper put each account.
type form1120Placement struct {
	// onLine maps an AccountID to the 1120 line it appears under.
	onLine map[string]string
	// unmapped holds the accounts in the "no 1120 line" section.
	unmapped map[string]bool
	// seen counts how many times each account is printed anywhere.
	seen map[string]int
}

func form1120Placements(env form1120Envelope) form1120Placement {
	p := form1120Placement{
		onLine:   map[string]string{},
		unmapped: map[string]bool{},
		seen:     map[string]int{},
	}
	if len(env.Reports) == 0 {
		return p
	}
	walkForm1120(env.Reports[0].Rows, nil, func(row form1120Row, trail []string) {
		if len(row.Cells) == 0 {
			return
		}
		id := form1120CellAccount(row.Cells[0])
		if id == "" {
			return
		}
		p.seen[id]++
		unmapped := false
		for _, title := range trail {
			if title == form1120UnmappedTitle {
				unmapped = true
			}
		}
		if unmapped {
			p.unmapped[id] = true
			return
		}
		if len(trail) > 0 {
			p.onLine[id] = trail[len(trail)-1]
		}
	})
	return p
}

// The workpaper's own repository type is referenced so the envelope's numbers
// can be compared without re-parsing strings when these assertions grow.
var _ = repository.Form1120SectionIncome
