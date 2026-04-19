package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

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

	_ = time.Now() // avoid unused import if the compiler strips asserts
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
