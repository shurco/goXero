package handlers

import (
	"fmt"
	"strconv"
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

// money formats a decimal the way Xero renders report amounts: two fixed
// decimal places, negative values with a leading minus, half away from zero.
// Banker's rounding (StringFixedBank) is *not* Xero's convention — it would
// round 2.345 down to 2.34 where Xero shows 2.35, and it is exactly the kind
// of one-cent disagreement that makes a report total differ from the sum of
// the rows above it.
func money(d decimal.Decimal) models.ReportCell {
	return models.ReportCell{Value: d.StringFixed(2)}
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

// trialBalanceMeasure returns the two amounts the YTD Debit / YTD Credit columns
// carry for one account, and is the whole of what separates a meaningful Trial
// Balance from a self-consistent one.
//
// Xero's own Trial Balance prints two measures in one table: a profit-and-loss
// account is reported at its movement over the financial year, a balance-sheet
// account at its accumulated balance as at the report date. The measure follows
// the account's own class, so an account whose journals all predate the
// financial year — a liability brought forward, say — still appears, showing the
// balance it carries rather than a zero it never had.
//
// Choosing the measure is all that is left here: both are already netted per
// account — the repository splits the year-to-date movement, and a balance is
// netted by SplitSigned the same way — so the gross/net distinction lives in the
// query, and this is the P&L-versus-balance-sheet line Xero draws through the
// table. It cannot go: it is what keeps 840 Historical Adjustment (a liability
// whose journals all predate the year) in the report at its balance.
func trialBalanceMeasure(row repository.TrialBalanceRow) (debit, credit decimal.Decimal) {
	if repository.IsProfitAndLossAccountType(row.AccountType) {
		return row.YTDDebit, row.YTDCredit
	}
	return repository.SplitSigned(row.ClosingBalance)
}

// trialBalanceTitleNote states, in the report itself, which measure each pair of
// amount columns carries, and that each pair is a net. A reader checking the
// Total row against the ledger needs both halves of that sentence: the pairs are
// not the same measure, and a ledger page showing an account on both sides of
// the column does not print as two figures here.
const trialBalanceTitleNote = "Debit/Credit: net movement in the period, on the side it falls. " +
	"YTD Debit/YTD Credit: netted the same way, from the year-to-date movement for profit and loss " +
	"accounts and from the balance carried as at the report date for balance sheet accounts."

// renderTrialBalance groups TB rows by Account class (Revenue/Expense/Assets/…)
// following Xero's layout. The Debit/Credit columns cover the selected period;
// YTD Debit/YTD Credit carry the account's measure as described by
// trialBalanceTitleNote, and the Total row is the sum of the rows above it —
// checkable against the ledger rather than merely equal to itself.
func renderTrialBalance(orgName string, from, to time.Time, rows []repository.TrialBalanceRow) models.Report {
	r := models.Report{
		ReportID:     "TrialBalance",
		ReportName:   "Trial Balance",
		ReportType:   "TrialBalance",
		ReportTitles: []string{"Trial Balance", orgName, dateRangeLabel(from, to), trialBalanceTitleNote},
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
	// placed remembers which accounts already have a home, so an account type
	// none of the buckets above knows about is still rendered (by its own type
	// name) rather than silently dropped out of the report and its total.
	placed := map[uuid.UUID]bool{}
	appendRow := func(section *models.ReportRow, row repository.TrialBalanceRow, ytdDebit, ytdCredit decimal.Decimal) {
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				accountCell(row.AccountID, row.AccountCode, row.AccountName),
				money(row.Debit),
				money(row.Credit),
				money(ytdDebit),
				money(ytdCredit),
			},
		})
		placed[row.AccountID] = true
	}

	grandDebit, grandCredit := decimal.Zero, decimal.Zero
	grandYTDDebit, grandYTDCredit := decimal.Zero, decimal.Zero
	for _, b := range buckets {
		section := models.ReportRow{RowType: models.ReportRowTypeSection, Title: b.title}
		for _, row := range rows {
			if _, ok := b.types[row.AccountType]; !ok {
				continue
			}
			ytdDebit, ytdCredit := trialBalanceMeasure(row)
			// An account carrying a balance is rendered whatever period its journals
			// fall in; the measure above decides what the cells read, this decides
			// only that the account is not dropped.
			if row.Debit.IsZero() && row.Credit.IsZero() && ytdDebit.IsZero() && ytdCredit.IsZero() &&
				row.ClosingBalance.IsZero() {
				continue
			}
			appendRow(&section, row, ytdDebit, ytdCredit)
		}
		if len(section.Rows) == 0 {
			continue
		}
		totDR, totCR, totYTDR, totYTDC := sumTrialBalanceRows(section.Rows)
		section.Rows = append(section.Rows, summaryRow("Total "+b.title, totDR, totCR, totYTDR, totYTDC))
		r.Rows = append(r.Rows, section)
		grandDebit = grandDebit.Add(totDR)
		grandCredit = grandCredit.Add(totCR)
		grandYTDDebit = grandYTDDebit.Add(totYTDR)
		grandYTDCredit = grandYTDCredit.Add(totYTDC)
	}

	// Any account class the buckets above do not name, rendered under its own
	// type so the report still carries every account that has a balance.
	leftover := map[string]*models.ReportRow{}
	var leftoverOrder []string
	for _, row := range rows {
		if placed[row.AccountID] {
			continue
		}
		ytdDebit, ytdCredit := trialBalanceMeasure(row)
		// An account carrying a balance is rendered whatever period its journals
		// fall in; the measure above decides what the cells read, this decides
		// only that the account is not dropped.
		if row.Debit.IsZero() && row.Credit.IsZero() && ytdDebit.IsZero() && ytdCredit.IsZero() &&
			row.ClosingBalance.IsZero() {
			continue
		}
		section, ok := leftover[row.AccountType]
		if !ok {
			section = &models.ReportRow{RowType: models.ReportRowTypeSection, Title: row.AccountType}
			leftover[row.AccountType] = section
			leftoverOrder = append(leftoverOrder, row.AccountType)
		}
		appendRow(section, row, ytdDebit, ytdCredit)
	}
	for _, t := range leftoverOrder {
		section := *leftover[t]
		totDR, totCR, totYTDR, totYTDC := sumTrialBalanceRows(section.Rows)
		section.Rows = append(section.Rows, summaryRow("Total "+section.Title, totDR, totCR, totYTDR, totYTDC))
		r.Rows = append(r.Rows, section)
		grandDebit = grandDebit.Add(totDR)
		grandCredit = grandCredit.Add(totCR)
		grandYTDDebit = grandYTDDebit.Add(totYTDR)
		grandYTDCredit = grandYTDCredit.Add(totYTDC)
	}

	r.Rows = append(r.Rows, summaryRow("Total", grandDebit, grandCredit, grandYTDDebit, grandYTDCredit))
	return r
}

// sumTrialBalanceRows adds up the amount columns of rendered account rows, so a
// section total and the report total are the sum of what is printed above them.
func sumTrialBalanceRows(rows []models.ReportRow) (debit, credit, ytdDebit, ytdCredit decimal.Decimal) {
	for _, row := range rows {
		if len(row.Cells) < 5 {
			continue
		}
		debit = debit.Add(cellAmount(row.Cells[1]))
		credit = credit.Add(cellAmount(row.Cells[2]))
		ytdDebit = ytdDebit.Add(cellAmount(row.Cells[3]))
		ytdCredit = ytdCredit.Add(cellAmount(row.Cells[4]))
	}
	return debit, credit, ytdDebit, ytdCredit
}

// cellAmount reads back a rendered amount cell. Rows are summed from their own
// rendered cells rather than from a parallel accumulator, which is what makes
// the total provably the sum of the lines above it.
func cellAmount(c models.ReportCell) decimal.Decimal {
	if c.Value == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(c.Value)
	if err != nil {
		return decimal.Zero
	}
	return d
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

// agedCells renders the six amount columns of an aged row: Xero's five ageing
// columns and the row's own total.
//
// The cells go through the package's own money() renderer, so an aged report
// prints an amount exactly the way the Trial Balance or the P&L does: two
// decimals, no thousands separator, a nil bucket as "0.00". Xero's exported aged
// reports are *formatted* output — they group thousands and print a single dash
// for a nil cell — so a nil bucket reads "-" in
// docs/xero-reference/aged-*.txt and "0.00" here. That is a presentational
// difference and it is listed cell by cell in docs/xero-aged-parity.md; the two
// renderers were deliberately not split, because the same figure printing two
// ways in one product is a worse defect than a report differing from an export
// in its typography. (The front end is the other reason: ReportView.svelte
// decides a column is an amount column by testing each cell against
// /^-?\d[\d,]*(\.[\d]+)?$/, which a dash does not match, so dash-only columns
// would flip to left alignment in a way that changes with the data.)
func agedCells(amounts []decimal.Decimal) []models.ReportCell {
	cells := make([]models.ReportCell, 0, len(amounts))
	for _, a := range amounts {
		cells = append(cells, money(a))
	}
	return cells
}

// agedRow builds one data row of an aged report: a label in the first column
// and the five amounts plus the total after it.
func agedRow(label models.ReportCell, amounts []decimal.Decimal) models.ReportRow {
	return models.ReportRow{
		RowType: models.ReportRowTypeRow,
		Cells:   append([]models.ReportCell{label}, agedCells(amounts)...),
	}
}

// agedSummaryRow builds an aged report's Total row from the amounts it sums.
func agedSummaryRow(label string, amounts []decimal.Decimal) models.ReportRow {
	return models.ReportRow{
		RowType: models.ReportRowTypeSummary,
		Cells:   append([]models.ReportCell{txt(label)}, agedCells(amounts)...),
	}
}

// agedPercentRow renders Xero's "Percentage of total" row from the same amounts
// the Total row above it prints: every column as its share of the report's total,
// to two decimal places, so the Total column comes out at 100.00% by the same
// division as the rest rather than being typed in.
//
// When the denominator is nil there is no total to take a share of, so the row is
// empty rather than printed as a row of zeroes or of infinite shares: a report
// with nothing in it must not look as though it measured something.
//
// The percentages are computed from the amounts passed in, which are the ones the
// Total row printed, so the row can never disagree with the table above it.
func agedPercentRow(amounts []decimal.Decimal) models.ReportRow {
	row := models.ReportRow{RowType: models.ReportRowTypeSummary, Cells: []models.ReportCell{txt("Percentage of total")}}
	den := amounts[len(amounts)-1]
	if den.IsZero() {
		for range amounts {
			row.Cells = append(row.Cells, txt(""))
		}
		return row
	}
	for _, a := range amounts {
		pct := a.Div(den).Mul(decimal.NewFromInt(100))
		row.Cells = append(row.Cells, txt(pct.StringFixed(2)+"%"))
	}
	return row
}

// agedAdd accumulates one row's six amounts into a running total. The renderer
// uses it so every SummaryRow it prints is the sum of the rows rendered above it
// — the Total is the rows, not a second query that could drift from them.
func agedAdd(total, row []decimal.Decimal) []decimal.Decimal {
	for i := range total {
		total[i] = total[i].Add(row[i])
	}
	return total
}

// agedAmountsOf flattens an invoice row into the six amount columns.
func agedAmountsOf(row repository.AgedRow) []decimal.Decimal {
	return []decimal.Decimal{row.LessThan1Month, row.Month1, row.Month2, row.Month3, row.Older, row.Total}
}

// agedClaimAmountsOf flattens an expense-claim row into the same six columns.
func agedClaimAmountsOf(row repository.AgedClaimRow) []decimal.Decimal {
	return []decimal.Decimal{row.LessThan1Month, row.Month1, row.Month2, row.Month3, row.Older, row.Total}
}

// renderAged renders Xero's Aged Receivables Summary and Aged Payables Summary.
// The two share a table — Xero's five ageing columns, one row per contact — and
// differ in what the payables report adds around it:
//
//   - Receivables is that one table: header, the contact rows, a Total, and the
//     Percentage of total row.
//   - Payables is three blocks. The contact table appears under the heading "Aged
//     Payables"; an "Expense Claims" block follows it, one row per claimant, for
//     the organisation's unpaid expense claims; and the report's own Total is the
//     sum of the two blocks. Xero includes those claims in the payable because
//     they are one: docs/xero-reference/aged-payables-summary.txt prints 8,386.76
//     of trade payables plus 115.95 of expense claims as a Total of 8,502.71, and
//     its Percentage of total row is taken against that 8,502.71. A receivables
//     report gets no second block — Xero has no counterpart for one, and
//     inventing a block there would invent a figure.
//
// Every amount comes from the rows passed in: the block subtotals and the overall
// Total are accumulated from the rows rendered above them, so a Total that
// disagrees with its own columns is not expressible here.
//
// `claims` is the Expense Claims block's rows. It is nil for the receivables
// report; the block itself, and its subtotal, are printed only when it has rows.
func renderAged(reportID, reportName, orgName string, asOf time.Time, rows []repository.AgedRow, claims []repository.AgedClaimRow) models.Report {
	r := models.Report{
		ReportID:     reportID,
		ReportName:   reportName,
		ReportType:   reportID,
		ReportTitles: []string{reportName, orgName, "As at " + xeroDate(asOf)},
		ReportDate:   xeroDate(asOf),
	}
	r.Rows = []models.ReportRow{
		// Xero's five columns, in Xero's order. "< 1 Month" holds everything up
		// to 30 days past due — including what is not yet due — and "Older" is
		// everything past 120 days.
		headerRow("Contact", "< 1 Month", "1 Month", "2 Months", "3 Months", "Older", "Total"),
	}

	payables := reportID == "AgedPayables"
	contactTotals := make([]decimal.Decimal, 6)
	claimTotals := make([]decimal.Decimal, 6)

	contacts := models.ReportRow{RowType: models.ReportRowTypeSection}
	if payables {
		contacts.Title = "Aged Payables"
	}
	for _, row := range rows {
		amounts := agedAmountsOf(row)
		contacts.Rows = append(contacts.Rows, agedRow(contactCell(row.ContactID, row.ContactName), amounts))
		contactTotals = agedAdd(contactTotals, amounts)
	}
	if len(contacts.Rows) > 0 {
		r.Rows = append(r.Rows, contacts)
		if payables {
			r.Rows = append(r.Rows, agedSummaryRow("Total Aged Payables", contactTotals))
		}
	}

	if len(claims) > 0 {
		section := models.ReportRow{RowType: models.ReportRowTypeSection, Title: "Expense Claims"}
		for _, row := range claims {
			amounts := agedClaimAmountsOf(row)
			section.Rows = append(section.Rows, agedRow(txt(row.ClaimantName), amounts))
			claimTotals = agedAdd(claimTotals, amounts)
		}
		r.Rows = append(r.Rows, section, agedSummaryRow("Total Expense Claims", claimTotals))
	}

	// The report's own total: the two blocks added together. On the receivables
	// report there is only the one block, so this is the contact total.
	total := agedAdd(append([]decimal.Decimal{}, contactTotals...), claimTotals)
	r.Rows = append(r.Rows, agedSummaryRow("Total", total))
	r.Rows = append(r.Rows, agedPercentRow(total))
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

// renderExecutiveSummary matches Xero's two-column KPI layout. The column
// heading is derived from the requested window rather than hardcoded: Xero
// heads the column with the month the report covers.
func renderExecutiveSummary(orgName string, from, to time.Time, kpis []repository.ExecutiveKPI) models.Report {
	r := models.Report{
		ReportID:     "ExecutiveSummary",
		ReportName:   "Executive Summary",
		ReportType:   "ExecutiveSummary",
		ReportTitles: []string{"Executive Summary", orgName, "For the period ending " + xeroDate(to)},
		ReportDate:   xeroDate(to),
	}
	r.Rows = []models.ReportRow{
		headerRow("", periodColumnLabel(from, to)),
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

// periodColumnLabel names the window a KPI column covers: a whole month reads
// "March 2026", a longer window reads "1 January 2026 - 31 March 2026".
func periodColumnLabel(from, to time.Time) string {
	fullMonth := from.Day() == 1 && to.Equal(from.AddDate(0, 1, -1))
	if fullMonth && from.Year() == to.Year() {
		return from.Format("January 2006")
	}
	return from.Format("2 January 2006") + " - " + to.Format("2 January 2006")
}

// renderCashSummary renders Xero's Cash Summary: the period's cash movement,
// explained by the accounts the money moved to and from, closed with the bank
// balances either side of the window.
//
// It is not the Bank Summary. The Bank Summary states each bank account's
// opening, received, spent and closing figures; this report states where the
// cash went. Its Net Cash Movement row is built by summing the rows this
// renderer itself prints — income less expenses, plus other cash movements,
// plus net tax movements — and because every cash-moving
// journal's lines sum to zero, that sum is the change in the bank accounts'
// balances over the window. The renderer therefore never reads a movement
// figure from somewhere else and restates it: the row is an identity of the
// rows above it, and a client can check it against them.
func renderCashSummary(orgName string, from, to time.Time, cs *repository.CashSummaryReport) models.Report {
	r := models.Report{
		ReportID:   "CashSummary",
		ReportName: "Cash Summary",
		ReportType: "CashSummary",
		ReportTitles: []string{
			"Cash Summary", orgName, dateRangeLabel(from, to),
			cashSummaryCaveat(cs),
		},
		ReportDate: xeroDate(to),
	}
	// The period's own year labels its column, so the figure is always read
	// against the window the report is titled with. The three comparative
	// columns are present and empty: no comparative was requested and no budget
	// is stored, and an empty cell says that where a 0.00 would claim a
	// measurement the organisation has not taken.
	r.Rows = []models.ReportRow{
		headerRow("", strconv.Itoa(to.Year()), "Yearly average (YTD)", "Variance", "Variance for Variance"),
	}

	// Every account of a cash-moving journal lands in exactly one section, so
	// the sections partition the movement instead of approximating it. The
	// repository decides which: the split follows each account's own role, the
	// same way the accounting reports do, and looking through the control
	// accounts to the documents the cash settled is what leaves the sections
	// with the accounts the money was coded to rather than the control accounts
	// it passed through.
	var incomeRows, expenseRows, otherRows []repository.CashCategoryRow
	for _, row := range cs.Categories {
		switch row.Section {
		case repository.CashSectionIncome:
			incomeRows = append(incomeRows, row)
		case repository.CashSectionExpense:
			expenseRows = append(expenseRows, row)
		default:
			otherRows = append(otherRows, row)
		}
	}

	incomeMovement := cashSummarySection(&r, "Income", "Total Income", incomeRows, false)
	// The expenses print as positive amounts spent, the way the report reads
	// them, while their postings are signed like every other line — so each
	// section returns what its rows contribute to the movement (+300 in, −50
	// out) and prints the amounts the report shows. The surplus is therefore the
	// sum of the two contributions, which is the printed Total Income less the
	// printed Total Expenses.
	expenseMovement := cashSummarySection(&r, "Less Expenses", "Total Expenses", expenseRows, true)
	surplus := incomeMovement.Add(expenseMovement)
	r.Rows = append(r.Rows, cashSummaryAmountRow(txt("Surplus (Deficit)"), &surplus, models.ReportRowTypeRow))
	otherMovement := cashSummarySection(&r, "Plus Other Cash Movements", "Total Other Cash Movements", otherRows, false)

	// Xero splits the tax out of the movement into a section of its own. The
	// tax is not a posting to a tax account in this ledger — it is recorded
	// beside the net amount on the line that carries it — so each attributed
	// row hands its own tax here and keeps its net, and the tax a journal posts
	// a second time as a line on 820 Sales Tax is left out of both rather than
	// counted twice. Net Tax Movements is the sum of the two rows above it and
	// what closes the movement below, so the section is measured rather than
	// merely present.
	tax := models.ReportRow{
		RowType: models.ReportRowTypeSection,
		Title:   "Plus Tax Movements",
		Cells:   []models.ReportCell{txt("Plus Tax Movements")},
	}
	netTax := cs.TaxCollected.Add(cs.TaxPaid)
	for _, t := range []struct {
		label  string
		amount decimal.Decimal
	}{
		{"Tax Collected", cs.TaxCollected},
		{"Tax Paid", cs.TaxPaid},
		{"Net Tax Movements", netTax},
	} {
		amount := t.amount
		tax.Rows = append(tax.Rows, cashSummaryAmountRow(txt(t.label), &amount, models.ReportRowTypeRow))
	}
	r.Rows = append(r.Rows, tax)

	// The movement is the sum of the rows above it. Nothing here consults a
	// bank balance to decide what to print, so the row cannot disagree with the
	// rows it summarises.
	netMovement := surplus.Add(otherMovement).Add(netTax)
	r.Rows = append(r.Rows, cashSummaryAmountRow(txt("Net Cash Movement"), &netMovement, models.ReportRowTypeSummary))

	cashBalance := cs.OpeningBalance.Add(netMovement)
	summary := models.ReportRow{
		RowType: models.ReportRowTypeSection,
		Title:   "Summary",
		Cells:   []models.ReportCell{txt("Summary")},
	}
	summary.Rows = []models.ReportRow{
		cashSummaryAmountRow(txt("Opening Balance"), &cs.OpeningBalance, models.ReportRowTypeRow),
		cashSummaryAmountRow(txt("Plus Net Cash Movement"), &netMovement, models.ReportRowTypeRow),
		cashSummaryAmountRow(txt("Cash Balance"), &cashBalance, models.ReportRowTypeSummary),
	}
	r.Rows = append(r.Rows, summary)
	return r
}

// cashSummaryAmountRow builds one row of the Cash Summary's five-column grid: a
// label, the period's own figure, and the three comparative columns. A nil
// amount is rendered as an empty cell — the same treatment moneyPtr gives an
// absent comparative, and the reason an unmeasured line never reads as 0.00.
func cashSummaryAmountRow(label models.ReportCell, amount *decimal.Decimal, rowType string) models.ReportRow {
	row := models.ReportRow{RowType: rowType, Cells: []models.ReportCell{label, moneyPtr(amount)}}
	for i := 0; i < 3; i++ {
		row.Cells = append(row.Cells, models.ReportCell{Value: ""})
	}
	return row
}

// cashSummarySection appends one section of the Cash Summary and returns what
// its rows contribute to the period's movement, where a positive amount is cash
// in. With flip set the rows are printed with the opposite sign — the expenses,
// which the report shows as positive amounts — while the returned contribution
// keeps the movement's own sign, so the caller's arithmetic stays in one frame
// and the totals it prints are the sums of the rows above them.
func cashSummarySection(r *models.Report, title, totalLabel string, rows []repository.CashCategoryRow, flip bool) decimal.Decimal {
	section := models.ReportRow{
		RowType: models.ReportRowTypeSection,
		Title:   title,
		Cells:   []models.ReportCell{txt(title)},
	}
	movement := decimal.Zero
	for _, row := range rows {
		amount := row.Amount
		if flip {
			amount = amount.Neg()
		}
		label := accountCell(row.AccountID, row.AccountCode, row.AccountName)
		if row.Unattributed {
			// The account's own cash that this ledger links no document to. It
			// prints as the account the journal itself named and says so in the
			// row, so that a link the ledger does not hold is never read as a
			// coding the report chose; its total is in the titles.
			label.Value += " " + cashSummaryUnattributedMark
		}
		section.Rows = append(section.Rows,
			cashSummaryAmountRow(label, &amount, models.ReportRowTypeRow))
		movement = movement.Add(row.Amount)
	}
	total := movement
	if flip {
		total = total.Neg()
	}
	section.Rows = append(section.Rows, cashSummaryAmountRow(txt(totalLabel), &total, models.ReportRowTypeSummary))
	r.Rows = append(r.Rows, section)
	return movement
}

// cashSummaryUnattributedMark labels the row that carries cash this ledger
// links no document to, so a reader can tell a link the ledger does not hold
// from a coding the report chose.
const cashSummaryUnattributedMark = "(not attributed to a document)"

// cashSummaryCaveat names, in the report itself, the parts of Xero's Cash
// Summary layout this report leaves unmeasured, the one line it reads
// differently, and how much of the movement it could not attribute. The
// columns and headings stay in the payload so a client rendering the layout
// sees the gaps named rather than silently missing, and no figure is invented
// to fill one.
func cashSummaryCaveat(cs *repository.CashSummaryReport) string {
	return "Not represented: the Yearly average (YTD), Variance and Variance for Variance columns " +
		"(this report is rendered for a single period and the organisation stores no budget to " +
		"average or to compare against). Historical Adjustment is sectioned by its own account " +
		"class - it is a current liability, which is where Xero's Balance Sheet files it " +
		"(docs/xero-reference/balance-sheet.txt line 25) - so it prints in Plus Other Cash " +
		"Movements where Xero's own capture prints it inside Less Expenses " +
		"(docs/xero-reference/cash-summary.txt line 20); both are below the Surplus row, so the " +
		"movement and the balance are the same under either reading. Tax is measured rather than " +
		"assumed: each attributed line contributes the tax this ledger records beside its net to " +
		"Plus Tax Movements, and the tax a journal posts a second time as a line on 820 Sales Tax " +
		"is left out rather than counted twice. " + cashSummaryCoverage(cs)
}

// cashSummaryCoverage states, in the report itself, how much of the period's
// cash movement the ledger's own document links do not reach. It is a coverage
// statement and not a caveat on the totals: an unattributed amount is still in
// the sections above, on the account the journal itself named, so the report
// foots whether or not every link is there.
func cashSummaryCoverage(cs *repository.CashSummaryReport) string {
	unattributed := decimal.Zero
	for _, row := range cs.Categories {
		if row.Unattributed {
			unattributed = unattributed.Add(row.Amount)
		}
	}
	if unattributed.IsZero() {
		return "Coverage: the whole of the period's cash movement is attributed to the documents " +
			"the ledger links it to; no part of it is carried on a control account for want of a link."
	}
	return "Coverage: " + unattributed.StringFixed(2) + " of the period's cash movement is not " +
		"attributed to a document - the ledger links none to it - and is printed on the control " +
		"account the journal itself named, in a row marked " + cashSummaryUnattributedMark + "."
}

// renderSalesTax renders the sales-tax / BAS workpaper: one row per tax rate the
// organisation's own coded journals carry, and a Total row that is the sum of
// the rows above it.
//
// A rate's Tax Collected or Tax Paid cell is the rate's whole tax for the period
// or it is empty. It is never a part of one: the ledger does not record when a
// line's tax was merely unknown, so a figure summed over the lines that did
// happen to carry one would read as a measurement of the rate. Where a rate is
// short on a side, the Total row still sums the rates that measured it — a
// reader needs the figure — but its own label carries how much of the side it
// covers, and the caveat under the section names the lines left out. A total is
// never printed as if it were complete, and no cell reads 0.00 in place of a
// measurement.
func renderSalesTax(orgName, reportID, reportName string, from, to time.Time, rows []repository.SalesTaxRow) models.Report {
	r := models.Report{
		ReportID:     reportID,
		ReportName:   reportName,
		ReportType:   reportID,
		ReportTitles: []string{reportName, orgName, dateRangeLabel(from, to)},
		ReportDate:   xeroDate(to),
	}
	r.Rows = []models.ReportRow{
		headerRow("Tax Rate", "Net Sales", "Net Purchases", "Tax Collected", "Tax Paid", "Net Tax"),
	}

	var (
		netSales, netPurchases, taxCollected, taxPaid decimal.Decimal
		gap                                           = taxLedgerGap{
			collected: taxSideGap{name: "Tax Collected"},
			paid:      taxSideGap{name: "Tax Paid"},
		}
	)
	section := models.ReportRow{RowType: models.ReportRowTypeSection}
	for _, row := range rows {
		netSales = netSales.Add(row.NetSales)
		netPurchases = netPurchases.Add(row.NetPurchases)
		taxCollected = taxCollected.Add(row.TaxCollected)
		taxPaid = taxPaid.Add(row.TaxPaid)
		gap.add(row)
		// A side the rate never posted to has no tax to state: its cell stays
		// empty rather than reading 0.00, which would be a figure the ledger
		// never held for it. A side it did post to prints what the ledger
		// records there, however little that is.
		collected, paid := models.ReportCell{Value: ""}, models.ReportCell{Value: ""}
		if row.SalesLines > 0 {
			collected = money(row.TaxCollected)
		}
		if row.PurchaseLines > 0 {
			paid = money(row.TaxPaid)
		}
		section.Rows = append(section.Rows, models.ReportRow{
			RowType: models.ReportRowTypeRow,
			Cells: []models.ReportCell{
				txt(taxRateLabel(row)),
				money(row.NetSales),
				money(row.NetPurchases),
				collected,
				paid,
				money(row.TaxCollected.Sub(row.TaxPaid)),
			},
		})
	}

	total := models.ReportRow{
		RowType: models.ReportRowTypeSummary,
		Cells: []models.ReportCell{
			txt(totalLabel(gap)), money(netSales), money(netPurchases),
			models.ReportCell{Value: ""}, models.ReportCell{Value: ""}, models.ReportCell{Value: ""},
		},
	}
	// The tax columns are the whole of the tax the period's journal lines
	// record, so they are printed as soon as the period holds a tax-typed line
	// at all. A period that holds none has no figure to print and the cells
	// stay empty, with the caveat saying why.
	if len(rows) > 0 {
		total.Cells[3] = money(taxCollected)
		total.Cells[4] = money(taxPaid)
		total.Cells[5] = money(taxCollected.Sub(taxPaid))
	}
	if len(section.Rows) > 0 {
		section.Rows = append(section.Rows, total)
		r.Rows = append(r.Rows, section)
	} else {
		// No rate the organisation's own journals support: the report still
		// states its total, so a reader can tell an empty workpaper from a
		// truncated one.
		r.Rows = append(r.Rows, total)
	}
	r.Rows = append(r.Rows, models.ReportRow{
		RowType: models.ReportRowTypeRow,
		Cells:   []models.ReportCell{txt(taxColumnsCaveat(len(rows), gap))},
	})
	return r
}

// taxLedgerGap is the ledger's own hole in the two tax columns: the tax-typed
// lines it records no tax for, side by side. It is not a shortfall in the
// report — the report prints every figure the ledger holds — but it is what a
// reader has to know before reading either column as complete.
type taxLedgerGap struct {
	collected taxSideGap
	paid      taxSideGap
}

// taxSideGap is one column's hole: how many of its tax-typed lines record no
// tax, and the rates those lines name.
type taxSideGap struct {
	name  string
	lines int64
	rates []string
}

// add folds one rate's counts into the gap.
func (g *taxLedgerGap) add(row repository.SalesTaxRow) {
	if row.SalesLines > row.MeasuredSales {
		g.collected.lines += row.SalesLines - row.MeasuredSales
		g.collected.rates = append(g.collected.rates, taxRateLabel(row))
	}
	if row.PurchaseLines > row.MeasuredPurchases {
		g.paid.lines += row.PurchaseLines - row.MeasuredPurchases
		g.paid.rates = append(g.paid.rates, taxRateLabel(row))
	}
}

// lines is the whole of the gap, both columns together.
func (g taxLedgerGap) lines() int64 { return g.collected.lines + g.paid.lines }

// sides returns the columns that have a hole, in report order.
func (g taxLedgerGap) sides() []taxSideGap {
	var out []taxSideGap
	for _, side := range []taxSideGap{g.collected, g.paid} {
		if side.lines > 0 {
			out = append(out, side)
		}
	}
	return out
}

// totalLabel names the Total row. Its figures are the whole of the tax the
// ledger records, so the label says so whenever the ledger records none for a
// line that names a rate: a reader seeing the bare word "Total" is reading a
// column nothing is missing from.
func totalLabel(gap taxLedgerGap) string {
	if gap.lines() == 0 {
		return "Total"
	}
	return "Total (" + taxGapClause(gap.lines()) + ")"
}

// taxGapClause states a line count as the ledger's hole.
func taxGapClause(n int64) string {
	if n == 1 {
		return "1 tax-typed line records no tax"
	}
	return strconv.FormatInt(n, 10) + " tax-typed lines record no tax"
}

// taxRateLabel names a row the way the organisation names the rate: its own
// name when it has defined one, otherwise the tax type the journals carry.
func taxRateLabel(row repository.SalesTaxRow) string {
	if row.TaxRateName != "" {
		return row.TaxRateName
	}
	return row.TaxType
}

// taxColumnsCaveat states, in the report body, what the two tax columns are —
// the whole of the tax the period's journal lines record, line by line — and,
// where the ledger records no tax at all for a line that names a rate, which
// column and which rates those lines are in and that their 0.00 is the whole of
// what the ledger holds for them. The figures are never adjusted for it: an
// unrecorded tax is missing from the ledger, and a report that guessed at it
// would be inventing the figure the ledger does not have.
func taxColumnsCaveat(rows int, gap taxLedgerGap) string {
	switch {
	case rows == 0:
		return "No tax rate is represented: no posted journal line in this period carries a tax type."
	case gap.lines() == 0:
		return "Tax Collected and Tax Paid are the tax the period's posted journal lines record, " +
			"line by line. Every line that names a rate records a tax — a 0% rate's 0.00 is its own " +
			"recorded answer — so both columns and Net Tax are the whole of what the ledger holds."
	default:
		parts := make([]string, 0, 2)
		for _, side := range gap.sides() {
			parts = append(parts, side.name+": "+pluralLines(side.lines)+" naming a rate, under "+joinLabels(side.rates))
		}
		return "Tax Collected and Tax Paid are the tax the period's posted journal lines record, " +
			"line by line: a 0.00 is printed where the ledger records one, a 0% rate's own answer " +
			"among them. The ledger records no tax at all for " + strings.Join(parts, "; ") +
			". Those lines contribute nothing to the figures above, which are therefore the whole of " +
			"the tax the ledger records; where one of them should have carried tax, that column is " +
			"wrong by it and the missing figure is the ledger's, not the report's."
	}
}

// pluralLines renders a line count with its noun.
func pluralLines(n int64) string {
	if n == 1 {
		return "1 line"
	}
	return strconv.FormatInt(n, 10) + " lines"
}

// joinLabels names a list in prose: "A", "A and B", "A, B and C".
func joinLabels(labels []string) string {
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	default:
		return strings.Join(labels[:len(labels)-1], ", ") + " and " + labels[len(labels)-1]
	}
}

// renderAgedByContact renders the per-contact drill-down: one section per
// contact holding that contact's outstanding invoices, a subtotal per contact
// and a grand total. It carries the same ageing columns as the summary report,
// so the two agree column by column. It is not the summary: the summary has one
// row per contact and a Percentage of total row, this has one row per invoice.
func renderAgedByContact(reportID, reportName, orgName string, asOf time.Time, rows []repository.AgedDetailRow) models.Report {
	r := models.Report{
		ReportID:     reportID,
		ReportName:   reportName,
		ReportType:   reportID,
		ReportTitles: []string{reportName, orgName, "As at " + xeroDate(asOf)},
		ReportDate:   xeroDate(asOf),
	}
	r.Rows = []models.ReportRow{
		// The same five columns as the summary report, so the two agree column
		// by column. Xero's by-contact report has no reference file in
		// docs/xero-reference, so nothing is added here that the summary's own
		// reference does not carry — in particular no Percentage of total row.
		headerRow("Contact", "< 1 Month", "1 Month", "2 Months", "3 Months", "Older", "Total"),
	}
	grand := make([]decimal.Decimal, 6)

	var section *models.ReportRow
	var sectionContact uuid.UUID
	var sectionTotals []decimal.Decimal
	flush := func() {
		if section == nil {
			return
		}
		section.Rows = append(section.Rows, summaryRow("Total "+section.Title, sectionTotals...))
		r.Rows = append(r.Rows, *section)
		section = nil
	}
	for _, row := range rows {
		if section == nil || row.ContactID != sectionContact {
			flush()
			sectionContact = row.ContactID
			sectionTotals = make([]decimal.Decimal, 6)
			section = &models.ReportRow{
				RowType: models.ReportRowTypeSection,
				Title:   row.ContactName,
				Cells:   []models.ReportCell{contactCell(row.ContactID, row.ContactName)},
			}
		}
		amounts := []decimal.Decimal{row.LessThan1Month, row.Month1, row.Month2, row.Month3, row.Older, row.Total}
		cells := []models.ReportCell{txt(invoiceLabel(row))}
		for _, a := range amounts {
			cells = append(cells, money(a))
		}
		section.Rows = append(section.Rows, models.ReportRow{RowType: models.ReportRowTypeRow, Cells: cells})
		for i, a := range amounts {
			sectionTotals[i] = sectionTotals[i].Add(a)
			grand[i] = grand[i].Add(a)
		}
	}
	flush()
	r.Rows = append(r.Rows, summaryRow("Total", grand...))
	return r
}

// invoiceLabel identifies one invoice inside its contact's block: its number
// where it has one — it is optional — and its date, which is what the ageing
// falls back to when the invoice carries no due date.
func invoiceLabel(row repository.AgedDetailRow) string {
	if row.InvoiceNumber == "" {
		return xeroDate(row.Date)
	}
	return row.InvoiceNumber + " " + xeroDate(row.Date)
}

// budgetSummaryCaveat states, in the report itself, why the report has no rows.
// It is the fourth ReportTitles entry — the same place the Cash Summary names
// what it does not represent — and the reports page prints the titles under the
// heading, so a reader is told rather than left to infer it from an empty table.
func budgetSummaryCaveat() string {
	return "No budget is stored: this organisation has no budget in goXero (there is no budget " +
		"table in the schema), so there is no budget figure to show and none to measure " +
		"actuals against. Xero returns the same empty report for an organisation with no budget set."
}

// renderBudgetSummary renders Xero's Budget Summary for an organisation with no
// budget. The envelope, the report's identity and the header row are Xero's, so
// a client parses the shape it expects; what the report does not do is pretend
// to a measurement. It used to print a single `Total 0.00` row under the header,
// which is not a total of anything — there are no accounts above it and no
// budget behind it — and a report that shows a confident zero where nothing was
// measured is worse than one that says what it is missing (the caveat above).
//
// `from` is the window the caller asked for, nil when they asked for none. A
// report with no rows still has to be honest about which period it is being read
// at: with an explicit window the title names that window, and without one it
// keeps Xero's own year wording, so the default reading is unchanged.
func renderBudgetSummary(orgName string, from *time.Time, to time.Time) models.Report {
	period := "For the year to " + xeroDate(to)
	if from != nil {
		period = dateRangeLabel(*from, to)
	}
	r := models.Report{
		ReportID:     "BudgetSummary",
		ReportName:   "Budget Summary",
		ReportType:   "BudgetSummary",
		ReportTitles: []string{"Budget Summary", orgName, period, budgetSummaryCaveat()},
		ReportDate:   xeroDate(to),
		Rows:         []models.ReportRow{headerRow("Account", "Budget")},
	}
	return r
}
