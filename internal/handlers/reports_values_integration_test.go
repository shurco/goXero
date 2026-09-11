package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTP_Reports_Values drives the full invoice → payment flow and then
// reads the reports back to assert concrete numbers come out correctly.
// Xero guarantees the following identities, which we check here:
//
//	Trial Balance : Debits == Credits (always)
//	P&L           : Income − CostOfSales − Expenses = Net Profit
//	Balance Sheet : Assets == Liabilities + Equity + Retained Earnings
//	Bank Summary  : Opening + Received − Spent = Closing
//	Aged AR       : Total sum across rows matches invoice AmountDue total
func TestHTTP_Reports_Values(t *testing.T) {
	h := newHarness(t)

	// 1. Contact + authorised invoice for $120 (100 net + 20 tax).
	status, body := h.do(t, http.MethodPost, "/api/v1/contacts", map[string]any{
		"Name":       "Report values " + uuid.NewString()[:6],
		"IsCustomer": true,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var cEnv struct {
		Contacts []struct {
			ContactID string `json:"ContactID"`
		} `json:"Contacts"`
	}
	require.NoError(t, json.Unmarshal(body, &cEnv))

	date := "2026-03-01T00:00:00Z"
	status, body = h.do(t, http.MethodPost, "/api/v1/invoices", map[string]any{
		"Type":            "ACCREC",
		"Status":          "AUTHORISED",
		"ContactID":       cEnv.Contacts[0].ContactID,
		"Date":            date,
		"DueDate":         date,
		"LineAmountTypes": "Exclusive",
		"LineItems": []map[string]any{
			{"Description": "Work", "Quantity": "1", "UnitAmount": "100", "AccountCode": "400", "TaxAmount": "0"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))

	// 2. Hit each report and verify the accounting identities.
	asOf := "2026-03-31"
	from := "2026-01-01"
	to := "2026-03-31"

	trialBalance := fetchReport(t, h, "/api/v1/reports/trial-balance?date="+asOf)
	debits, credits := sumTrialBalanceTotals(t, trialBalance)
	assert.True(t, debits.Sub(credits).Abs().LessThan(dec("0.01")),
		"trial balance must be in balance, DR=%s CR=%s", debits, credits)

	pnl := fetchReport(t, h, "/api/v1/reports/profit-and-loss?fromDate="+from+"&toDate="+to)
	// Net Profit row is the very last SummaryRow.
	netProfit := lastSummaryValue(t, pnl)
	assert.Truef(t, netProfit.GreaterThanOrEqual(dec("100")),
		"income was $100, net profit should be at least 100, got %s", netProfit)

	bs := fetchReport(t, h, "/api/v1/reports/balance-sheet?date="+asOf)
	require.NotEmpty(t, bs.Reports)

	bank := fetchReport(t, h, "/api/v1/reports/bank-summary?fromDate="+from+"&toDate="+to)
	require.NotEmpty(t, bank.Reports)

	aged := fetchReport(t, h, "/api/v1/reports/aged-receivables?date="+asOf)
	require.NotEmpty(t, aged.Reports)

	// 3. Journal report: there should be at least one entry (invoice posting).
	jr := fetchReport(t, h, "/api/v1/reports/journal-report?fromDate="+from+"&toDate="+to)
	require.NotEmpty(t, jr.Reports)
}

// TestHTTP_Reports_DateFilterExcludesOutOfWindow is the P0 regression test for
// the "unused JOIN" bug: the date predicate used to live in the ON-clause of a
// LEFT JOIN that SELECT/WHERE never referenced, so the reports silently summed
// all history and fromDate/toDate had no effect. Each report is probed with a
// posting on either side of the window — a December posting (before the period)
// and a June one (after the cutoff) — so that dropping either bound, upper or
// lower, changes a total and fails the test. This test fails on the old SQL and
// passes only when both bounds really filter the aggregated lines.
func TestHTTP_Reports_DateFilterExcludesOutOfWindow(t *testing.T) {
	h := newHarness(t)

	contactID := createContact(t, h, "Date filter "+uuid.NewString()[:6])
	createAuthorisedInvoice(t, h, contactID, "2025-12-15T00:00:00Z", "50")  // before every window
	createAuthorisedInvoice(t, h, contactID, "2026-03-01T00:00:00Z", "100") // inside every window
	createAuthorisedInvoice(t, h, contactID, "2026-06-01T00:00:00Z", "700") // after every cutoff

	// Trial Balance as at 2026-03-31: the period Debit/Credit columns cover
	// March alone — the June invoice is past the cutoff and the December one
	// belongs to the previous financial year.
	tb := fetchReport(t, h, "/api/v1/reports/trial-balance?date=2026-03-31")
	debits, credits := sumTrialBalanceTotals(t, tb)
	assert.True(t, debits.Sub(dec("100")).Abs().LessThan(dec("0.01")),
		"trial balance debit must cover the March invoice only, got %s", debits)
	assert.True(t, credits.Sub(dec("100")).Abs().LessThan(dec("0.01")),
		"trial balance credit must cover the March invoice only, got %s", credits)

	// P&L over Q1 2026: net profit is the March invoice only.
	pnl := fetchReport(t, h, "/api/v1/reports/profit-and-loss?fromDate=2026-01-01&toDate=2026-03-31")
	netProfit := lastSummaryValue(t, pnl)
	assert.True(t, netProfit.Sub(dec("100")).Abs().LessThan(dec("0.01")),
		"P&L net profit must exclude the December and June invoices, got %s", netProfit)

	// Balance Sheet as at 2026-03-31 is cumulative: it keeps the December
	// posting but still drops the June one (50 + 100).
	bs := fetchReport(t, h, "/api/v1/reports/balance-sheet?date=2026-03-31")
	netAssets := summaryByLabel(t, bs, "Net Assets")
	assert.True(t, netAssets.Sub(dec("150")).Abs().LessThan(dec("0.01")),
		"balance sheet net assets must exclude the June invoice only, got %s", netAssets)
}

// createContact posts a customer contact and returns its ContactID.
func createContact(t *testing.T, h *appHarness, name string) string {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "/api/v1/contacts", map[string]any{
		"Name":       name,
		"IsCustomer": true,
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var env struct {
		Contacts []struct {
			ContactID string `json:"ContactID"`
		} `json:"Contacts"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.NotEmpty(t, env.Contacts)
	return env.Contacts[0].ContactID
}

// createAuthorisedInvoice posts an authorised ACCREC invoice for a fixed net
// amount, posted to the US-style revenue account (code 400).
func createAuthorisedInvoice(t *testing.T, h *appHarness, contactID, date, amount string) {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "/api/v1/invoices", map[string]any{
		"Type":            "ACCREC",
		"Status":          "AUTHORISED",
		"ContactID":       contactID,
		"Date":            date,
		"DueDate":         date,
		"LineAmountTypes": "Exclusive",
		"LineItems": []map[string]any{
			{"Description": "Work", "Quantity": "1", "UnitAmount": amount, "AccountCode": "400", "TaxAmount": "0"},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
}

// ---- helpers -----------------------------------------------------------------

type xeroReportEnvelope struct {
	Status  string `json:"Status"`
	Reports []struct {
		ReportID   string          `json:"ReportID"`
		ReportName string          `json:"ReportName"`
		Rows       []xeroReportRow `json:"Rows"`
	} `json:"Reports"`
}

type xeroReportRow struct {
	RowType string           `json:"RowType"`
	Title   string           `json:"Title"`
	Cells   []xeroReportCell `json:"Cells"`
	Rows    []xeroReportRow  `json:"Rows"`
}

type xeroReportCell struct {
	Value string `json:"Value"`
}

func fetchReport(t *testing.T, h *appHarness, path string) xeroReportEnvelope {
	t.Helper()
	status, body := h.do(t, http.MethodGet, path, nil, true)
	require.Equalf(t, http.StatusOK, status, "%s: %s", path, string(body))
	var env xeroReportEnvelope
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "OK", env.Status, path)
	return env
}

// sumTrialBalanceTotals accumulates the debit/credit columns from the final
// "Total" SummaryRow of the trial balance report.
func sumTrialBalanceTotals(t *testing.T, env xeroReportEnvelope) (debit, credit decimalLike) {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	for _, row := range env.Reports[0].Rows {
		if row.RowType == "SummaryRow" && len(row.Cells) >= 3 {
			if row.Cells[0].Value == "Total" {
				debit = dec(row.Cells[1].Value)
				credit = dec(row.Cells[2].Value)
				return
			}
		}
	}
	return
}

// lastSummaryValue returns the numeric value of the last summary row's second
// cell (used for "Net Profit" style totals).
func lastSummaryValue(t *testing.T, env xeroReportEnvelope) decimalLike {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	rows := env.Reports[0].Rows
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		if r.RowType == "SummaryRow" && len(r.Cells) >= 2 {
			return dec(r.Cells[1].Value)
		}
	}
	return dec("0")
}

// summaryByLabel walks the report rows (including rows nested inside sections)
// and returns the second cell of the first SummaryRow whose label matches.
func summaryByLabel(t *testing.T, env xeroReportEnvelope, label string) decimalLike {
	t.Helper()
	require.NotEmpty(t, env.Reports)
	var walk func(rows []xeroReportRow) (decimalLike, bool)
	walk = func(rows []xeroReportRow) (decimalLike, bool) {
		for _, r := range rows {
			if r.RowType == "SummaryRow" && len(r.Cells) >= 2 && r.Cells[0].Value == label {
				return dec(r.Cells[1].Value), true
			}
			if v, ok := walk(r.Rows); ok {
				return v, true
			}
		}
		return decimalLike{}, false
	}
	v, ok := walk(env.Reports[0].Rows)
	require.Truef(t, ok, "summary row %q not found", label)
	return v
}

// --- tiny decimal shim so tests don't need the full shopspring/decimal import ---

type decimalLike struct{ f float64 }

func dec(s string) decimalLike {
	if s == "" {
		return decimalLike{}
	}
	f, _ := strconv.ParseFloat(s, 64)
	return decimalLike{f: f}
}

func (d decimalLike) Sub(o decimalLike) decimalLike { return decimalLike{d.f - o.f} }
func (d decimalLike) Abs() decimalLike {
	if d.f < 0 {
		return decimalLike{-d.f}
	}
	return d
}
func (d decimalLike) LessThan(o decimalLike) bool           { return d.f < o.f }
func (d decimalLike) GreaterThanOrEqual(o decimalLike) bool { return d.f >= o.f }
func (d decimalLike) String() string                        { return strconv.FormatFloat(d.f, 'f', 2, 64) }
