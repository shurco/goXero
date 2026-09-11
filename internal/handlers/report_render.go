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

// moneyPtr renders an optional comparative amount. When no comparative period
// was requested the cell is left empty, so the same renderer serves both the
// plain and the comparative report.
func moneyPtr(d *decimal.Decimal) models.ReportCell {
	if d == nil {
		return models.ReportCell{Value: ""}
	}
	return money(*d)
}

// summaryRowPtr is summaryRow for comparatives: the trailing optional amount
// is rendered as a plain cell, preserving the SummaryRow row type.
func summaryRowPtr(label string, base decimal.Decimal, cmp *decimal.Decimal) models.ReportRow {
	return models.ReportRow{
		RowType: models.ReportRowTypeSummary,
		Cells:   []models.ReportCell{txt(label), money(base), moneyPtr(cmp)},
	}
}

// hasComparative reports whether a comparative column should be rendered.
func hasComparative(ptr *time.Time) bool { return ptr != nil }

// dateRangeLabel renders "From 1 January 2026 To 31 March 2026".
func dateRangeLabel(from, to time.Time) string {
	return fmt.Sprintf("From %s To %s", xeroDate(from), xeroDate(to))
}

// xeroDate formats a date the way the Xero reports page shows it: "2 April 2026".
func xeroDate(t time.Time) string { return t.Format("2 January 2006") }

// renderTrialBalance groups TB rows by Account class (Revenue/Expense/Assets/…)
// following Xero's layout. The Debit/Credit columns cover the selected period;
// YTD Debit/YTD Credit cover the financial year through the period end.
func renderTrialBalance(orgName string, from, to time.Time, rows []repository.TrialBalanceRow) models.Report {
	r := models.Report{
		ReportID:     "TrialBalance",
		ReportName:   "Trial Balance",
		ReportType:   "TrialBalance",
		ReportTitles: []string{"Trial Balance", orgName, dateRangeLabel(from, to)},
		ReportDate:   xeroDate(to),
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
	grandYTDDebit, grandYTDCredit := decimal.Zero, decimal.Zero
	for _, b := range buckets {
		section := models.ReportRow{RowType: models.ReportRowTypeSection, Title: b.title}
		totDR, totCR := decimal.Zero, decimal.Zero
		totYTDR, totYTDC := decimal.Zero, decimal.Zero
		for _, row := range rows {
			if _, ok := b.types[row.AccountType]; !ok {
				continue
			}
			if row.Debit.IsZero() && row.Credit.IsZero() &&
				row.YTDDebit.IsZero() && row.YTDCredit.IsZero() {
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
			totYTDR = totYTDR.Add(row.YTDDebit)
			totYTDC = totYTDC.Add(row.YTDCredit)
		}
		if len(section.Rows) > 0 {
			section.Rows = append(section.Rows, summaryRow("Total "+b.title, totDR, totCR, totYTDR, totYTDC))
			r.Rows = append(r.Rows, section)
			grandDebit = grandDebit.Add(totDR)
			grandCredit = grandCredit.Add(totCR)
			grandYTDDebit = grandYTDDebit.Add(totYTDR)
			grandYTDCredit = grandYTDCredit.Add(totYTDC)
		}
	}
	r.Rows = append(r.Rows, summaryRow("Total", grandDebit, grandCredit, grandYTDDebit, grandYTDCredit))
	return r
}

func setOf(s ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		out[v] = struct{}{}
	}
	return out
}

// renderProfitAndLoss renders the repo PnLReport as Xero-shaped rows. When a
// comparative period was requested, a second amount column is emitted for
// every line, section total and headline figure.
func renderProfitAndLoss(orgName string, rep *repository.PnLReport) models.Report {
	r := models.Report{
		ReportID:     "ProfitAndLoss",
		ReportName:   "Profit and Loss",
		ReportType:   "ProfitAndLoss",
		ReportTitles: []string{"Profit and Loss", orgName, dateRangeLabel(rep.From, rep.To)},
		ReportDate:   xeroDate(rep.To),
	}
	if hasComparative(rep.ComparativeTo) {
		r.Rows = []models.ReportRow{headerRow("", xeroDate(rep.To), xeroDate(*rep.ComparativeTo))}
	} else {
		r.Rows = []models.ReportRow{headerRow("", xeroDate(rep.To))}
	}

	sec := func(title string, rows []repository.PnLRow, total decimal.Decimal, cmpTotal *decimal.Decimal) models.ReportRow {
		s := models.ReportRow{RowType: models.ReportRowTypeSection, Title: title}
		for _, row := range rows {
			s.Rows = append(s.Rows, models.ReportRow{
				RowType: models.ReportRowTypeRow,
				Cells: []models.ReportCell{
					accountCell(row.AccountID, row.AccountCode, row.AccountName),
					money(row.Amount),
					moneyPtr(row.Comparative),
				},
			})
		}
		s.Rows = append(s.Rows, summaryRowPtr("Total "+strings.ToLower(title), total, cmpTotal))
		return s
	}
	r.Rows = append(r.Rows, sec("Income", rep.Income, rep.TotalIncome, rep.ComparativeIncome))
	if len(rep.CostOfSales) > 0 {
		r.Rows = append(r.Rows, sec("Less Cost of Sales", rep.CostOfSales, rep.TotalCostOfSales, rep.ComparativeCostSales))
	}
	// Xero always prints Gross Profit, even when there is no Cost of Sales.
	r.Rows = append(r.Rows, summaryRowPtr("Gross Profit", rep.GrossProfit, rep.ComparativeGross))
	r.Rows = append(r.Rows, sec("Less Operating Expenses", rep.Expenses, rep.TotalExpenses, rep.ComparativeExpenses))
	r.Rows = append(r.Rows, summaryRowPtr("Net Profit", rep.NetProfit, rep.ComparativeNet))
	return r
}

// renderBalanceSheet renders the repo BalanceSheet in Xero's format. With
// `?compare=true` the second column carries the beginning-of-year (or explicit
// comparative) balances — the two columns IRS Schedule L asks for.
func renderBalanceSheet(orgName string, bs *repository.BalanceSheet) models.Report {
	r := models.Report{
		ReportID:     "BalanceSheet",
		ReportName:   "Balance Sheet",
		ReportType:   "BalanceSheet",
		ReportTitles: []string{"Balance Sheet", orgName, "As at " + xeroDate(bs.AsOf)},
		ReportDate:   xeroDate(bs.AsOf),
	}
	if hasComparative(bs.ComparativeAsOf) {
		r.Rows = []models.ReportRow{headerRow("", xeroDate(bs.AsOf), xeroDate(*bs.ComparativeAsOf))}
	} else {
		r.Rows = []models.ReportRow{headerRow("", xeroDate(bs.AsOf))}
	}

	accountRow := func(row repository.BalanceSheetRow) models.ReportRow {
		return models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				accountCell(row.AccountID, row.AccountCode, row.AccountName),
				money(row.Amount),
				moneyPtr(row.Comparative),
			},
		}
	}
	section := func(title string, rows []repository.BalanceSheetRow, total decimal.Decimal, cmpTotal *decimal.Decimal) models.ReportRow {
		s := models.ReportRow{RowType: models.ReportRowTypeSection, Title: title}
		for _, row := range rows {
			s.Rows = append(s.Rows, accountRow(row))
		}
		s.Rows = append(s.Rows, summaryRowPtr("Total "+title, total, cmpTotal))
		return s
	}
	r.Rows = append(r.Rows, section("Assets", bs.Assets, bs.TotalAssets, bs.ComparativeTotalAssets))
	r.Rows = append(r.Rows, section("Liabilities", bs.Liabilities, bs.TotalLiabilities, bs.ComparativeTotalLiabilities))
	// Equity lists its accounts plus the retained-earnings roll-up, then one
	// Total Equity row — built explicitly so it does not borrow the generic
	// section's trailing total.
	equity := models.ReportRow{RowType: models.ReportRowTypeSection, Title: "Equity"}
	for _, row := range bs.Equity {
		equity.Rows = append(equity.Rows, accountRow(row))
	}
	equity.Rows = append(equity.Rows,
		models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				txt("Retained Earnings"),
				money(bs.RetainedEarnings),
				moneyPtr(bs.ComparativeRetainedEarnings),
			},
		},
		summaryRowPtr("Total Equity", bs.TotalEquity, bs.ComparativeTotalEquity),
	)
	r.Rows = append(r.Rows, equity)
	netAssets := bs.TotalAssets.Sub(bs.TotalLiabilities)
	var cmpNetAssets *decimal.Decimal
	if bs.ComparativeTotalAssets != nil && bs.ComparativeTotalLiabilities != nil {
		v := bs.ComparativeTotalAssets.Sub(*bs.ComparativeTotalLiabilities)
		cmpNetAssets = &v
	}
	r.Rows = append(r.Rows, summaryRowPtr("Net Assets", netAssets, cmpNetAssets))
	return r
}

// renderAccountTransactions renders the raw GL postings of the selected
// accounts — the drill-down behind every other report.
func renderAccountTransactions(orgName string, from, to time.Time, lines []repository.JournalFeedRow) models.Report {
	r := models.Report{
		ReportID:     "AccountTransactions",
		ReportName:   "Account Transactions",
		ReportType:   "AccountTransactions",
		ReportTitles: []string{"Account Transactions", orgName, dateRangeLabel(from, to)},
		ReportDate:   xeroDate(to),
		Rows: []models.ReportRow{
			headerRow("Date", "Source", "Reference", "Description", "Account", "Debit", "Credit"),
		},
	}
	section := models.ReportRow{RowType: models.ReportRowTypeSection}
	totalDebit, totalCredit := decimal.Zero, decimal.Zero
	for _, l := range lines {
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				txt(l.Date.Format("2006-01-02")),
				txt(l.Source),
				txt(l.Reference),
				txt(l.Description),
				accountCell(l.AccountID, l.AccountCode, l.AccountName),
				money(l.Debit),
				money(l.Credit),
			},
		})
		totalDebit = totalDebit.Add(l.Debit)
		totalCredit = totalCredit.Add(l.Credit)
	}
	if len(section.Rows) > 0 {
		section.Rows = append(section.Rows, summaryRow("Total", totalDebit, totalCredit))
		r.Rows = append(r.Rows, section)
	}
	return r
}

// renderGeneralLedgerDetail renders one block per account: opening balance,
// postings at a running balance, then the closing balance.
func renderGeneralLedgerDetail(orgName string, from, to time.Time, groups []repository.GLAccountGroup) models.Report {
	r := models.Report{
		ReportID:     "GeneralLedgerDetail",
		ReportName:   "General Ledger Detail",
		ReportType:   "GeneralLedgerDetail",
		ReportTitles: []string{"General Ledger Detail", orgName, dateRangeLabel(from, to)},
		ReportDate:   xeroDate(to),
		Rows: []models.ReportRow{
			headerRow("Date", "Source", "Reference", "Description", "Debit", "Credit", "Balance"),
		},
	}
	for _, g := range groups {
		if len(g.Lines) == 0 && g.Opening.IsZero() && g.Closing.IsZero() {
			continue
		}
		section := models.ReportRow{
			RowType: models.ReportRowTypeSection,
			Title:   fmt.Sprintf("%s (%s)", g.AccountName, g.AccountCode),
			Cells: []models.ReportCell{
				accountCell(g.AccountID, g.AccountCode, g.AccountName),
			},
		}
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				txt(""), txt(""), txt(""), txt("Opening Balance"),
				txt(""), txt(""), money(g.Opening),
			},
		})
		for _, l := range g.Lines {
			section.Rows = append(section.Rows, models.ReportRow{
				RowType: models.ReportRowTypeRow,
				Cells: []models.ReportCell{
					txt(l.Date.Format("2006-01-02")),
					txt(l.Source),
					txt(l.Reference),
					txt(l.Description),
					money(l.Debit),
					money(l.Credit),
					money(l.Balance),
				},
			})
		}
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeSummary,
			Cells: []models.ReportCell{
				txt("Closing Balance"), txt(""), txt(""), txt(""),
				money(g.Debit), money(g.Credit), money(g.Closing),
			},
		})
		r.Rows = append(r.Rows, section)
	}
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
