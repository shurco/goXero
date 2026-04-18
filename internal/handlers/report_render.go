package handlers

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/repository"
)

// renderXeroReport wraps the typed repo reports into the canonical Xero
// Reporting API shape so clients (SDKs, xlsx exporters) can parse them
// without special-casing. Every report is returned as a singleton list
// under `Reports: [...]`.
func renderXeroReport(r models.Report) models.ReportsEnvelope {
	if r.Fields == nil {
		r.Fields = []any{}
	}
	if r.UpdatedDateUTC.IsZero() {
		r.UpdatedDateUTC = time.Now().UTC()
	}
	return models.ReportsEnvelope{
		ID:           uuid.NewString(),
		Status:       "OK",
		ProviderName: "goxero",
		DateTimeUTC:  time.Now().UTC(),
		Reports:      []models.Report{r},
	}
}

// txt returns a simple Value-only cell.
func txt(v string) models.ReportCell { return models.ReportCell{Value: v} }

// money formats a decimal like Xero (plain "%.2f", negatives get a leading minus).
func money(d decimal.Decimal) models.ReportCell {
	return models.ReportCell{Value: d.StringFixedBank(2)}
}

// accountCell renders an account label with the AccountID attribute Xero
// exposes so deep-link navigation continues to work in the frontend.
func accountCell(accID uuid.UUID, code, name string) models.ReportCell {
	label := name
	if code != "" {
		label = fmt.Sprintf("%s (%s)", name, code)
	}
	return models.ReportCell{
		Value: label,
		Attributes: []models.ReportCellAttribute{
			{ID: "account", Value: accID.String()},
		},
	}
}

// contactCell mirrors accountCell for ContactID attributes used in aged reports.
func contactCell(id uuid.UUID, name string) models.ReportCell {
	return models.ReportCell{
		Value: name,
		Attributes: []models.ReportCellAttribute{
			{ID: "contact", Value: id.String()},
		},
	}
}

// headerRow constructs a Header row from plain strings.
func headerRow(cells ...string) models.ReportRow {
	row := models.ReportRow{RowType: models.ReportRowTypeHeader}
	for _, c := range cells {
		row.Cells = append(row.Cells, txt(c))
	}
	return row
}

// summaryRow builds a SummaryRow with a label in the first column.
func summaryRow(label string, amounts ...decimal.Decimal) models.ReportRow {
	cells := []models.ReportCell{txt(label)}
	for _, a := range amounts {
		cells = append(cells, money(a))
	}
	return models.ReportRow{RowType: models.ReportRowTypeSummary, Cells: cells}
}

// xeroDate formats a date the way the Xero reports page shows it: "2 April 2026".
func xeroDate(t time.Time) string { return t.Format("2 January 2006") }

// renderTrialBalance groups TB rows by Account class (Revenue/Expense/Assets/…)
// following Xero's layout.
func renderTrialBalance(orgName string, asOf time.Time, rows []repository.TrialBalanceRow) models.Report {
	r := models.Report{
		ReportID:     "TrialBalance",
		ReportName:   "Trial Balance",
		ReportType:   "TrialBalance",
		ReportTitles: []string{"Trial Balance", orgName, "As at " + xeroDate(asOf)},
		ReportDate:   xeroDate(asOf),
	}
	r.Rows = []models.ReportRow{
		headerRow("Account", "Debit", "Credit", "YTD Debit", "YTD Credit"),
	}
	type bucket struct {
		title string
		types map[string]struct{}
	}
	buckets := []bucket{
		{"Revenue", setOf(models.AccountTypeRevenue, models.AccountTypeSales)},
		{"Less Cost of Sales", setOf(models.AccountTypeDirectCosts)},
		{"Less Operating Expenses", setOf(models.AccountTypeExpense, models.AccountTypeOverheads, models.AccountTypeDepreciatn, models.AccountTypeWages)},
		{"Assets", setOf(models.AccountTypeBank, models.AccountTypeCurrent, models.AccountTypeFixed, models.AccountTypePrepayment, models.AccountTypeInventory, models.AccountTypeNonCurrent)},
		{"Liabilities", setOf(models.AccountTypeCurrLiab, models.AccountTypeLiability, models.AccountTypeTermLiab, models.AccountTypePAYGLiab, models.AccountTypeSuperLiab)},
		{"Equity", setOf(models.AccountTypeEquity)},
	}
	grandDebit, grandCredit := decimal.Zero, decimal.Zero
	for _, b := range buckets {
		section := models.ReportRow{RowType: models.ReportRowTypeSection, Title: b.title}
		totDR, totCR := decimal.Zero, decimal.Zero
		for _, row := range rows {
			if _, ok := b.types[row.AccountType]; !ok {
				continue
			}
			if row.Debit.IsZero() && row.Credit.IsZero() {
				continue
			}
			section.Rows = append(section.Rows, models.ReportRow{
				RowType: models.ReportRowTypeRow,
				Cells: []models.ReportCell{
					accountCell(row.AccountID, row.AccountCode, row.AccountName),
					money(row.Debit),
					money(row.Credit),
					money(row.YTDDebit),
					money(row.YTDCredit),
				},
			})
			totDR = totDR.Add(row.Debit)
			totCR = totCR.Add(row.Credit)
		}
		if len(section.Rows) > 0 {
			section.Rows = append(section.Rows, summaryRow("Total "+b.title, totDR, totCR, totDR, totCR))
			r.Rows = append(r.Rows, section)
			grandDebit = grandDebit.Add(totDR)
			grandCredit = grandCredit.Add(totCR)
		}
	}
	r.Rows = append(r.Rows, summaryRow("Total", grandDebit, grandCredit, grandDebit, grandCredit))
	return r
}

func setOf(s ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		out[v] = struct{}{}
	}
	return out
}

// renderProfitAndLoss renders the repo PnLReport as Xero-shaped rows.
func renderProfitAndLoss(orgName string, rep *repository.PnLReport) models.Report {
	r := models.Report{
		ReportID:   "ProfitAndLoss",
		ReportName: "Profit and Loss",
		ReportType: "ProfitAndLoss",
		ReportTitles: []string{
			"Profit and Loss",
			orgName,
			fmt.Sprintf("From %s To %s", xeroDate(rep.From), xeroDate(rep.To)),
		},
		ReportDate: xeroDate(rep.To),
	}
	r.Rows = []models.ReportRow{headerRow("", xeroDate(rep.To))}

	sec := func(title string, rows []repository.PnLRow, total decimal.Decimal) models.ReportRow {
		s := models.ReportRow{RowType: models.ReportRowTypeSection, Title: title}
		for _, row := range rows {
			s.Rows = append(s.Rows, models.ReportRow{
				RowType: models.ReportRowTypeRow,
				Cells: []models.ReportCell{
					accountCell(row.AccountID, row.AccountCode, row.AccountName),
					money(row.Amount),
				},
			})
		}
		s.Rows = append(s.Rows, summaryRow("Total "+strings.ToLower(title), total))
		return s
	}
	r.Rows = append(r.Rows, sec("Income", rep.Income, rep.TotalIncome))
	if len(rep.CostOfSales) > 0 {
		r.Rows = append(r.Rows, sec("Less Cost of Sales", rep.CostOfSales, rep.TotalCostOfSales))
		r.Rows = append(r.Rows, summaryRow("Gross Profit", rep.GrossProfit))
	}
	r.Rows = append(r.Rows, sec("Less Operating Expenses", rep.Expenses, rep.TotalExpenses))
	r.Rows = append(r.Rows, summaryRow("Net Profit", rep.NetProfit))
	return r
}

// renderBalanceSheet renders the repo BalanceSheet in Xero's format.
func renderBalanceSheet(orgName string, bs *repository.BalanceSheet) models.Report {
	r := models.Report{
		ReportID:     "BalanceSheet",
		ReportName:   "Balance Sheet",
		ReportType:   "BalanceSheet",
		ReportTitles: []string{"Balance Sheet", orgName, "As at " + xeroDate(bs.AsOf)},
		ReportDate:   xeroDate(bs.AsOf),
	}
	r.Rows = []models.ReportRow{headerRow("", xeroDate(bs.AsOf))}

	section := func(title string, rows []repository.BalanceSheetRow, total decimal.Decimal) models.ReportRow {
		s := models.ReportRow{RowType: models.ReportRowTypeSection, Title: title}
		for _, row := range rows {
			s.Rows = append(s.Rows, models.ReportRow{
				RowType: models.ReportRowTypeRow,
				Cells: []models.ReportCell{
					accountCell(row.AccountID, row.AccountCode, row.AccountName),
					money(row.Amount),
				},
			})
		}
		s.Rows = append(s.Rows, summaryRow("Total "+title, total))
		return s
	}
	r.Rows = append(r.Rows, section("Assets", bs.Assets, bs.TotalAssets))
	r.Rows = append(r.Rows, section("Liabilities", bs.Liabilities, bs.TotalLiabilities))
	equityRow := section("Equity", bs.Equity, bs.TotalEquity)
	equityRow.Rows = append(equityRow.Rows[:len(equityRow.Rows)-1],
		models.ReportRow{RowType: models.ReportRowTypeRow, Cells: []models.ReportCell{
			txt("Retained Earnings"), money(bs.RetainedEarnings),
		}},
		summaryRow("Total Equity", bs.TotalEquity),
	)
	r.Rows = append(r.Rows, equityRow)
	r.Rows = append(r.Rows, summaryRow("Net Assets", bs.TotalAssets.Sub(bs.TotalLiabilities)))
	return r
}

// renderAged turns AgedRow slice into Xero AgedReceivables/AgedPayables.
func renderAged(reportID, reportName, orgName string, asOf time.Time, rows []repository.AgedRow) models.Report {
	r := models.Report{
		ReportID:     reportID,
		ReportName:   reportName,
		ReportType:   reportID,
		ReportTitles: []string{reportName, orgName, "As at " + xeroDate(asOf)},
		ReportDate:   xeroDate(asOf),
	}
	r.Rows = []models.ReportRow{
		headerRow("Contact", "Current", "< 30", "31-60", "61-90", "> 90", "Total"),
	}
	section := models.ReportRow{RowType: models.ReportRowTypeSection}
	var gc, g1, g2, g3, g4, gt decimal.Decimal
	for _, row := range rows {
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				contactCell(row.ContactID, row.ContactName),
				money(row.Current), money(row.Days1To30),
				money(row.Days31To60), money(row.Days61To90),
				money(row.Days91Plus), money(row.Total),
			},
		})
		gc, g1, g2, g3, g4, gt =
			gc.Add(row.Current), g1.Add(row.Days1To30),
			g2.Add(row.Days31To60), g3.Add(row.Days61To90),
			g4.Add(row.Days91Plus), gt.Add(row.Total)
	}
	if len(section.Rows) > 0 {
		r.Rows = append(r.Rows, section)
	}
	r.Rows = append(r.Rows, summaryRow("Total", gc, g1, g2, g3, g4, gt))
	return r
}

// renderBankSummary — opening / received / spent / closing per bank account.
func renderBankSummary(orgName string, from, to time.Time, rows []repository.BankSummaryRow) models.Report {
	r := models.Report{
		ReportID:   "BankSummary",
		ReportName: "Bank Summary",
		ReportType: "BankSummary",
		ReportTitles: []string{
			"Bank Summary",
			orgName,
			fmt.Sprintf("From %s To %s", xeroDate(from), xeroDate(to)),
		},
		ReportDate: xeroDate(to),
	}
	r.Rows = []models.ReportRow{
		headerRow("Bank Account", "Opening Balance", "Cash Received", "Cash Spent", "Closing Balance"),
	}
	section := models.ReportRow{RowType: models.ReportRowTypeSection}
	var op, cr, cs, cl decimal.Decimal
	for _, row := range rows {
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				accountCell(row.AccountID, row.AccountCode, row.AccountName),
				money(row.OpeningBalance), money(row.CashReceived),
				money(row.CashSpent), money(row.ClosingBalance),
			},
		})
		op = op.Add(row.OpeningBalance)
		cr = cr.Add(row.CashReceived)
		cs = cs.Add(row.CashSpent)
		cl = cl.Add(row.ClosingBalance)
	}
	if len(section.Rows) > 0 {
		r.Rows = append(r.Rows, section)
	}
	r.Rows = append(r.Rows, summaryRow("Total", op, cr, cs, cl))
	return r
}

// renderExecutiveSummary matches Xero's two-column KPI layout.
func renderExecutiveSummary(orgName string, to time.Time, kpis []repository.ExecutiveKPI) models.Report {
	r := models.Report{
		ReportID:     "ExecutiveSummary",
		ReportName:   "Executive Summary",
		ReportType:   "ExecutiveSummary",
		ReportTitles: []string{"Executive Summary", orgName, "For the period ending " + xeroDate(to)},
		ReportDate:   xeroDate(to),
	}
	r.Rows = []models.ReportRow{
		headerRow("", "This month"),
	}
	section := models.ReportRow{RowType: models.ReportRowTypeSection, Title: "Key Performance Indicators"}
	for _, k := range kpis {
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells:   []models.ReportCell{txt(k.Title), money(k.Value)},
		})
	}
	r.Rows = append(r.Rows, section)
	return r
}

// renderCashSummary — inflows/outflows per bank account over the period.
func renderCashSummary(orgName string, from, to time.Time, rows []repository.BankSummaryRow) models.Report {
	rep := renderBankSummary(orgName, from, to, rows)
	rep.ReportID = "CashSummary"
	rep.ReportName = "Cash Summary"
	rep.ReportType = "CashSummary"
	rep.ReportTitles[0] = "Cash Summary"
	return rep
}

// renderBudgetSummary is a stub mirroring Xero's layout (no budget data stored
// yet — we return an empty report so consumers can still parse the shape).
func renderBudgetSummary(orgName string, to time.Time) models.Report {
	r := models.Report{
		ReportID:     "BudgetSummary",
		ReportName:   "Budget Summary",
		ReportType:   "BudgetSummary",
		ReportTitles: []string{"Budget Summary", orgName, "For the year to " + xeroDate(to)},
		ReportDate:   xeroDate(to),
		Rows:         []models.ReportRow{headerRow("Account", "Budget"), summaryRow("Total", decimal.Zero)},
	}
	return r
}
