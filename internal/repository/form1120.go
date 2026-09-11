package repository

// Form 1120 workpaper — the figures of IRS Form 1120, Page 1, plus Schedule L
// and Schedule M-1, assembled by *re-grouping* what the existing reports
// already produce. There is no new SQL here: the income and deduction lines
// read ProfitAndLoss and the balance sheet sections read BalanceSheet, exactly
// as the Profit & Loss and Balance Sheet endpoints render them. The 1120 is a
// different arrangement of those numbers, not a different set of queries.
//
// One table at the bottom of this file is the report's *data*, not its logic:
//
//	Form1120LineCatalogue — every line of the form in form order, with the
//	                        arithmetic that rolls the sub-totals up.
//
// The other data — which account's figure goes on which line — is not in this
// file. A placement is a fact about an organisation's own chart, so it is a row
// of the organisation's own table, report_line_mappings (migration 00028), read
// by form1120Mappings below. This file states only the five placements that
// hold for *any* chart, because Xero's account type settles them on its own.
// Nothing here names an account code or an account name.
//
// Nothing here encodes a tax rule beyond the arithmetic printed on the form
// itself and the two catch-alls the form keeps for what it does not name: page
// 1 line 26 "Other deductions" for the expenses, and Schedule L line L18 "Other
// current liabilities" for the liabilities. Every account in the chart the
// database seeds reaches a line through one of those, so the "no 1120 line"
// section of the report renders empty. It is kept anyway: it is the workpaper's
// own check, and it is what tells the accountant the return is not ready when a
// chart grows an account nothing places.
//
// References:
//   https://www.irs.gov/forms-pubs/about-form-1120
//   https://www.irs.gov/instructions/i1120

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

// Section identifiers. A line belongs to exactly one section; the handler
// prints the sections in the order Form1120SectionOrder lists them.
const (
	Form1120SectionIncome     = "income"
	Form1120SectionDeductions = "deductions"
	Form1120SectionTax        = "tax"
	Form1120SectionScheduleL  = "schedule_l"
	Form1120SectionScheduleM1 = "schedule_m1"
)

// Basis tells the report which figure a line takes from the accounts it owns:
// the movement over the reporting period (income and deductions) or the closing
// balance, with a beginning-of-year column beside it (Schedule L).
const (
	Form1120BasisPeriod  = "period"
	Form1120BasisClosing = "closing"
)

// Form1120ReportID is the report's key in report_line_mappings, and the
// ReportID the handler publishes it under. It is one constant so the table the
// organisation edits and the report that reads it cannot drift apart.
const Form1120ReportID = "Form1120"

// Form1120LineDef is one line of the workpaper.
type Form1120LineDef struct {
	// ID is the line as the form prints it: "1a", "13", "31" for Page 1, "L15"
	// for Schedule L, "M1" for Schedule M-1.
	ID string
	// Section groups the line on the page.
	Section string
	// Label is the line's wording, as close to the form's own as it can be
	// without reproducing the form.
	Label string
	// Basis is Form1120BasisPeriod or Form1120BasisClosing.
	Basis string
	// Formula names the lines this one adds up, in order; a leading "-"
	// subtracts. A line with no formula is either funded by the mapping table
	// or — like line 31 — left for the accountant to complete.
	Formula []string
	// Note is printed under the line when it needs saying out loud.
	Note string
}

// Form1120Mapping is one placement of an account on a 1120 line.
//
// Type matches the account's Xero type and an empty Type means "any type".
// AccountID pins the placement to one account, and is how every row of
// report_line_mappings arrives. A placement pinned to an account beats a
// type-wide one, so a chart can put every SALES account on line 1a and still
// send one specific revenue account elsewhere.
//
// There is deliberately no code field. A code is the organisation's own label
// for an account and Xero lets it be anything, so a table keyed on codes
// describes one chart and misdescribes the next one silently.
type Form1120Mapping struct {
	Type      string
	AccountID uuid.UUID
	Line      string
	// Why records the reasoning, so the next reader can tell a considered
	// mapping from a guess.
	Why string
}

// Form1120Account is one account contributing to a line. Only the figures the
// account actually carries are populated: income-statement accounts have no
// beginning or closing balance, and a balance-sheet account's Period is the
// movement that moved its opening balance to its closing one.
type Form1120Account struct {
	AccountID uuid.UUID       `json:"AccountID"`
	Code      string          `json:"AccountCode"`
	Name      string          `json:"AccountName"`
	Type      string          `json:"AccountType"`
	Period    decimal.Decimal `json:"PeriodAmount"`
	Beginning decimal.Decimal `json:"BeginningBalance"`
	Closing   decimal.Decimal `json:"ClosingBalance"`
}

// hasBalance reports whether the account carries any figure at all. Accounts
// that are completely empty are not printed anywhere — a workpaper that lists
// the whole chart of accounts at 0.00 buries the lines that matter.
func (a Form1120Account) hasBalance() bool {
	return !a.Period.IsZero() || !a.Beginning.IsZero() || !a.Closing.IsZero()
}

// Form1120Line is one rendered line: the accounts behind it and the figure that
// goes on the form.
type Form1120Line struct {
	ID      string `json:"Line"`
	Section string `json:"Section"`
	Label   string `json:"Label"`
	Basis   string `json:"Basis"`
	Note    string `json:"Note,omitempty"`
	// Amount is the figure printed on the line: the period movement for the
	// income and deduction lines, the closing balance for Schedule L.
	Amount decimal.Decimal `json:"Amount"`
	// Beginning is Schedule L's beginning-of-year column; zero everywhere else.
	Beginning decimal.Decimal   `json:"BeginningBalance"`
	Accounts  []Form1120Account `json:"Accounts,omitempty"`
}

// Form1120Report is the whole workpaper.
type Form1120Report struct {
	// From and To are the tax period the income and deduction lines cover.
	From time.Time `json:"FromDate"`
	To   time.Time `json:"ToDate"`
	// BeginsOn is the day before From — the date Schedule L's beginning-of-year
	// column is measured at.
	BeginsOn          time.Time         `json:"BeginningOfYear"`
	Lines             []Form1120Line    `json:"Lines"`
	Unmapped          []Form1120Account `json:"UnmappedAccounts"`
	TotalIncome       decimal.Decimal   `json:"TotalIncome"`
	TotalDeductions   decimal.Decimal   `json:"TotalDeductions"`
	TaxableIncome     decimal.Decimal   `json:"TaxableIncome"`
	NetIncomePerBooks decimal.Decimal   `json:"NetIncomePerBooks"`
}

// Form1120 builds the workpaper for the period `p`.
//
// The income and deduction lines are the period movement of the accounts
// ProfitAndLoss reports; Schedule L is the per-account balance sheet with the
// beginning-of-year column that BalanceSheet already computes when asked for a
// comparative date. Every account that carries a figure but that the mapping
// table does not place is returned in Unmapped.
func (r *ReportRepository) Form1120(ctx context.Context, orgID uuid.UUID, p Period) (*Form1120Report, error) {
	if p.To.Before(p.From) {
		return nil, fmt.Errorf("form 1120: period end %s is before period start %s",
			p.To.Format(time.DateOnly), p.From.Format(time.DateOnly))
	}
	pinned, err := r.form1120Mappings(ctx, orgID)
	if err != nil {
		return nil, err
	}
	pnl, err := r.ProfitAndLoss(ctx, orgID, p, nil)
	if err != nil {
		return nil, err
	}
	// Schedule L's beginning-of-year column is the balance the books carried
	// *into* the period, so it is measured on the last day of the year before.
	// BalanceSheet's comparative column is inclusive of its as-at date, which
	// is why this is the day before From and not From itself: a posting made on
	// the first day of the tax year belongs to the year, not to the opening
	// balance.
	beginsOn := p.From.AddDate(0, 0, -1)
	bs, err := r.BalanceSheet(ctx, orgID, p.To, &beginsOn)
	if err != nil {
		return nil, err
	}

	// The account universe: the two reports between them cover every account in
	// the chart of accounts, whether or not it has any activity.
	accounts := make(map[uuid.UUID]*Form1120Account)
	// Income-statement accounts — ProfitAndLoss carries their period movement
	// (revenue already flipped to a positive figure).
	addPnL := func(rows []PnLRow) {
		for _, row := range rows {
			accounts[row.AccountID] = &Form1120Account{
				AccountID: row.AccountID,
				Code:      row.AccountCode,
				Name:      row.AccountName,
				Type:      row.AccountType,
				Period:    row.Amount,
			}
		}
	}
	addPnL(pnl.Income)
	addPnL(pnl.CostOfSales)
	addPnL(pnl.Expenses)
	// Balance-sheet accounts — BalanceSheet carries the closing balance and, in
	// its comparative column, the balance the period opened with.
	addBS := func(rows []BalanceSheetRow) {
		for _, row := range rows {
			a := &Form1120Account{
				AccountID: row.AccountID,
				Code:      row.AccountCode,
				Name:      row.AccountName,
				Type:      row.AccountType,
				Closing:   row.Amount,
			}
			if row.Comparative != nil {
				a.Beginning = *row.Comparative
			}
			a.Period = a.Closing.Sub(a.Beginning)
			accounts[row.AccountID] = a
		}
	}
	addBS(bs.Assets)
	addBS(bs.Liabilities)
	addBS(bs.Equity)

	sorted := make([]*Form1120Account, 0, len(accounts))
	for _, a := range accounts {
		sorted = append(sorted, a)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Code != sorted[j].Code {
			return sorted[i].Code < sorted[j].Code
		}
		return sorted[i].Name < sorted[j].Name
	})

	// Lay the catalogue out first, so the report always shows the whole form —
	// a line with nothing behind it prints at zero rather than disappearing.
	lines := make([]Form1120Line, 0, len(Form1120LineCatalogue))
	at := make(map[string]int, len(Form1120LineCatalogue))
	for _, def := range Form1120LineCatalogue {
		lines = append(lines, Form1120Line{
			ID: def.ID, Section: def.Section, Label: def.Label, Basis: def.Basis, Note: def.Note,
		})
		at[def.ID] = len(lines) - 1
	}

	placed := make(map[uuid.UUID]bool, len(accounts))
	for _, a := range sorted {
		if !a.hasBalance() {
			continue
		}
		mapping := form1120MappingFor(a, pinned)
		if mapping == nil {
			continue
		}
		i, ok := at[mapping.Line]
		if !ok {
			// The mapping points at a line this catalogue does not print; let
			// the account fall through to the "no 1120 line" section rather
			// than vanish.
			continue
		}
		line := &lines[i]
		line.Accounts = append(line.Accounts, *a)
		if line.Basis == Form1120BasisClosing {
			line.Amount = line.Amount.Add(a.Closing)
			line.Beginning = line.Beginning.Add(a.Beginning)
		} else {
			line.Amount = line.Amount.Add(a.Period)
		}
		placed[a.AccountID] = true
	}

	// Two lines have no accounts of their own.
	//
	// Schedule L's unappropriated retained earnings is the equity account plus
	// the profit the books have not yet closed into it; BalanceSheet computes
	// that roll-up for us and exposes it as RetainedEarnings.
	if i, ok := at["L24"]; ok {
		opening := decimal.Zero
		if bs.ComparativeRetainedEarnings != nil {
			opening = *bs.ComparativeRetainedEarnings
		}
		lines[i].Accounts = append(lines[i].Accounts, Form1120Account{
			Name:      "Retained earnings (profit and loss not yet closed to an equity account)",
			Period:    bs.RetainedEarnings.Sub(opening),
			Beginning: opening,
			Closing:   bs.RetainedEarnings,
		})
		lines[i].Amount = lines[i].Amount.Add(bs.RetainedEarnings)
		lines[i].Beginning = lines[i].Beginning.Add(opening)
	}
	// Schedule M-1 opens with the book result, which is the Profit & Loss
	// bottom line rather than any one account.
	if i, ok := at["M1"]; ok {
		lines[i].Accounts = append(lines[i].Accounts, Form1120Account{
			Name:    "Net income per books (Profit and Loss for the period)",
			Period:  pnl.NetProfit,
			Closing: pnl.NetProfit,
		})
		lines[i].Amount = pnl.NetProfit
	}

	// The catalogue is in form order and every formula only refers to lines
	// above it, so one forward pass is enough.
	for i, def := range Form1120LineCatalogue {
		if len(def.Formula) == 0 {
			continue
		}
		amount, beginning := decimal.Zero, decimal.Zero
		for _, ref := range def.Formula {
			sign := decimal.NewFromInt(1)
			id := ref
			if strings.HasPrefix(ref, "-") {
				sign = decimal.NewFromInt(-1)
				id = strings.TrimPrefix(ref, "-")
			}
			j, ok := at[id]
			if !ok {
				continue
			}
			amount = amount.Add(lines[j].Amount.Mul(sign))
			beginning = beginning.Add(lines[j].Beginning.Mul(sign))
		}
		lines[i].Amount = amount
		lines[i].Beginning = beginning
	}

	report := &Form1120Report{
		From:              p.From,
		To:                p.To,
		BeginsOn:          beginsOn,
		Lines:             lines,
		NetIncomePerBooks: pnl.NetProfit,
	}
	report.TotalIncome = lineAmount(lines, at, "11")
	report.TotalDeductions = lineAmount(lines, at, "27")
	report.TaxableIncome = lineAmount(lines, at, "30")
	// Line 31 is deliberately blank: goXero stores no tax rate, so the report
	// states the figure the rate applies to and leaves the multiplication to
	// the accountant.
	if i, ok := at["31"]; ok {
		lines[i].Note = fmt.Sprintf(
			"Taxable income is %s. Apply the current corporate income tax rate to that figure — goXero stores no tax rate, so this line is left for you to complete.",
			report.TaxableIncome.StringFixed(2))
	}

	for _, a := range sorted {
		if placed[a.AccountID] || !a.hasBalance() {
			continue
		}
		report.Unmapped = append(report.Unmapped, *a)
	}
	return report, nil
}

// lineAmount reads a line's figure, or zero when the catalogue has no such line
// (the catalogue is data and a reader may have edited it).
func lineAmount(lines []Form1120Line, at map[string]int, id string) decimal.Decimal {
	i, ok := at[id]
	if !ok {
		return decimal.Zero
	}
	return lines[i].Amount
}

// form1120Mappings reads the organisation's own placements: the rows of
// report_line_mappings that say which line of the form each of its accounts goes
// on. They are the organisation's data, so the report places the accounts of the
// chart it is actually reading rather than the accounts of the chart this
// program was written against.
func (r *ReportRepository) form1120Mappings(ctx context.Context, orgID uuid.UUID) ([]Form1120Mapping, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT account_id, line, COALESCE(reason, '')
		   FROM report_line_mappings
		  WHERE organisation_id = $1 AND report = $2
		  ORDER BY line, account_id`, orgID, Form1120ReportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Form1120Mapping, 0, 64)
	for rows.Next() {
		var m Form1120Mapping
		if err := rows.Scan(&m.AccountID, &m.Line, &m.Why); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// form1120MappingFor returns the mapping that places the account, or nil when
// nothing places it. The organisation's own placement wins over a type-wide
// rule, so one account can be pulled out of its type's default line; the
// type-wide rules are the five in Form1120Mappings that hold for any chart.
func form1120MappingFor(a *Form1120Account, pinned []Form1120Mapping) *Form1120Mapping {
	for i := range pinned {
		if pinned[i].AccountID == a.AccountID {
			return &pinned[i]
		}
	}
	var typeWide *Form1120Mapping
	for i := range Form1120Mappings {
		m := &Form1120Mappings[i]
		if m.Type != "" && m.Type != a.Type {
			continue
		}
		if typeWide == nil {
			typeWide = m
		}
	}
	return typeWide
}

// Form1120LineCatalogue is the form, in its own order. It is data: add a line,
// relabel one, change a formula, and the report follows. The renderer prints
// every line here, including the ones nothing is mapped to, so the workpaper
// always shows the whole of Page 1.
//
// The line numbers and wording follow Form 1120, Page 1. Line 31 is left blank
// on purpose — see the repository's Form1120 method.
var Form1120LineCatalogue = []Form1120LineDef{
	// ── Income ─────────────────────────────────────────────────────────────
	{ID: "1a", Section: Form1120SectionIncome, Label: "Gross receipts", Basis: Form1120BasisPeriod},
	{ID: "1b", Section: Form1120SectionIncome, Label: "Returns and allowances", Basis: Form1120BasisPeriod},
	{ID: "1c", Section: Form1120SectionIncome, Label: "Balance", Basis: Form1120BasisPeriod, Formula: []string{"1a", "-1b"}},
	{ID: "2", Section: Form1120SectionIncome, Label: "Cost of goods sold", Basis: Form1120BasisPeriod},
	{ID: "3", Section: Form1120SectionIncome, Label: "Gross profit", Basis: Form1120BasisPeriod, Formula: []string{"1c", "-2"}},
	{ID: "4", Section: Form1120SectionIncome, Label: "Dividends", Basis: Form1120BasisPeriod},
	{ID: "5", Section: Form1120SectionIncome, Label: "Interest", Basis: Form1120BasisPeriod},
	{ID: "6", Section: Form1120SectionIncome, Label: "Gross rents", Basis: Form1120BasisPeriod},
	{ID: "7", Section: Form1120SectionIncome, Label: "Gross royalties", Basis: Form1120BasisPeriod},
	{ID: "8", Section: Form1120SectionIncome, Label: "Capital gain net income", Basis: Form1120BasisPeriod},
	{ID: "9", Section: Form1120SectionIncome, Label: "Net gain", Basis: Form1120BasisPeriod},
	{ID: "10", Section: Form1120SectionIncome, Label: "Other income", Basis: Form1120BasisPeriod},
	{ID: "11", Section: Form1120SectionIncome, Label: "Total income", Basis: Form1120BasisPeriod,
		Formula: []string{"3", "4", "5", "6", "7", "8", "9", "10"}},

	// ── Deductions ─────────────────────────────────────────────────────────
	{ID: "12", Section: Form1120SectionDeductions, Label: "Compensation of officers", Basis: Form1120BasisPeriod},
	{ID: "13", Section: Form1120SectionDeductions, Label: "Salaries and wages", Basis: Form1120BasisPeriod},
	{ID: "14", Section: Form1120SectionDeductions, Label: "Repairs and maintenance", Basis: Form1120BasisPeriod},
	{ID: "15", Section: Form1120SectionDeductions, Label: "Bad debts", Basis: Form1120BasisPeriod},
	{ID: "16", Section: Form1120SectionDeductions, Label: "Rents", Basis: Form1120BasisPeriod},
	{ID: "17", Section: Form1120SectionDeductions, Label: "Taxes and licences", Basis: Form1120BasisPeriod},
	{ID: "18", Section: Form1120SectionDeductions, Label: "Interest", Basis: Form1120BasisPeriod},
	{ID: "19", Section: Form1120SectionDeductions, Label: "Charitable contributions", Basis: Form1120BasisPeriod},
	{ID: "20", Section: Form1120SectionDeductions, Label: "Depreciation", Basis: Form1120BasisPeriod},
	{ID: "21", Section: Form1120SectionDeductions, Label: "Depletion", Basis: Form1120BasisPeriod},
	{ID: "22", Section: Form1120SectionDeductions, Label: "Advertising", Basis: Form1120BasisPeriod},
	{ID: "23", Section: Form1120SectionDeductions, Label: "Pension and profit sharing", Basis: Form1120BasisPeriod},
	{ID: "24", Section: Form1120SectionDeductions, Label: "Employee benefit programmes", Basis: Form1120BasisPeriod},
	{ID: "25", Section: Form1120SectionDeductions, Label: "Domestic production activities deduction", Basis: Form1120BasisPeriod},
	{ID: "26", Section: Form1120SectionDeductions, Label: "Other deductions", Basis: Form1120BasisPeriod,
		Note: "Page 1 names no line for the accounts printed here — cleaning, entertainment, light, power and heating, motor vehicle, office, printing and stationery, telephone and internet, travel, and the rest of the chart's unnamed expenses — so Other deductions is where the form puts them. Entertainment is carried at its full book figure: only the part the meals and entertainment limit allows may go on the form, and the disallowed part belongs on Schedule M-1 line 5."},
	{ID: "27", Section: Form1120SectionDeductions, Label: "Total deductions", Basis: Form1120BasisPeriod,
		Formula: []string{"12", "13", "14", "15", "16", "17", "18", "19", "20", "21", "22", "23", "24", "25", "26"}},

	// ── Tax and taxable income ─────────────────────────────────────────────
	{ID: "28", Section: Form1120SectionTax, Label: "Taxable income before net operating loss deduction", Basis: Form1120BasisPeriod,
		Formula: []string{"11", "-27"}},
	{ID: "29", Section: Form1120SectionTax, Label: "Net operating loss deduction", Basis: Form1120BasisPeriod},
	{ID: "30", Section: Form1120SectionTax, Label: "Taxable income", Basis: Form1120BasisPeriod, Formula: []string{"28", "-29"}},
	{ID: "31", Section: Form1120SectionTax, Label: "Total tax (apply the current rate)", Basis: Form1120BasisPeriod},

	// ── Schedule L — balance sheet per books ───────────────────────────────
	{ID: "L1", Section: Form1120SectionScheduleL, Label: "Cash", Basis: Form1120BasisClosing},
	{ID: "L2", Section: Form1120SectionScheduleL, Label: "Trade notes and accounts receivable", Basis: Form1120BasisClosing},
	{ID: "L3", Section: Form1120SectionScheduleL, Label: "Inventories", Basis: Form1120BasisClosing},
	{ID: "L4", Section: Form1120SectionScheduleL, Label: "US government obligations", Basis: Form1120BasisClosing},
	{ID: "L5", Section: Form1120SectionScheduleL, Label: "Tax-exempt securities", Basis: Form1120BasisClosing},
	{ID: "L6", Section: Form1120SectionScheduleL, Label: "Other current assets", Basis: Form1120BasisClosing},
	{ID: "L7", Section: Form1120SectionScheduleL, Label: "Loans to shareholders", Basis: Form1120BasisClosing},
	{ID: "L8", Section: Form1120SectionScheduleL, Label: "Mortgage and real estate loans", Basis: Form1120BasisClosing},
	{ID: "L9", Section: Form1120SectionScheduleL, Label: "Other investments", Basis: Form1120BasisClosing},
	{ID: "L10a", Section: Form1120SectionScheduleL, Label: "Buildings and other depreciable assets", Basis: Form1120BasisClosing},
	{ID: "L10b", Section: Form1120SectionScheduleL, Label: "Less accumulated depreciation", Basis: Form1120BasisClosing},
	{ID: "L11", Section: Form1120SectionScheduleL, Label: "Depletable assets", Basis: Form1120BasisClosing},
	{ID: "L12", Section: Form1120SectionScheduleL, Label: "Land", Basis: Form1120BasisClosing},
	{ID: "L13", Section: Form1120SectionScheduleL, Label: "Intangible assets (amortizable only)", Basis: Form1120BasisClosing},
	{ID: "L14", Section: Form1120SectionScheduleL, Label: "Other assets", Basis: Form1120BasisClosing},
	{ID: "L15", Section: Form1120SectionScheduleL, Label: "Total assets", Basis: Form1120BasisClosing,
		Formula: []string{"L1", "L2", "L3", "L4", "L5", "L6", "L7", "L8", "L9", "L10a", "L10b", "L11", "L12", "L13", "L14"}},
	{ID: "L16", Section: Form1120SectionScheduleL, Label: "Accounts payable", Basis: Form1120BasisClosing},
	{ID: "L17", Section: Form1120SectionScheduleL, Label: "Mortgages, notes and bonds payable in less than 1 year", Basis: Form1120BasisClosing},
	{ID: "L18", Section: Form1120SectionScheduleL, Label: "Other current liabilities", Basis: Form1120BasisClosing},
	{ID: "L19", Section: Form1120SectionScheduleL, Label: "Mortgages, notes and bonds payable in 1 year or more", Basis: Form1120BasisClosing},
	{ID: "L20", Section: Form1120SectionScheduleL, Label: "Other liabilities", Basis: Form1120BasisClosing},
	{ID: "L21", Section: Form1120SectionScheduleL, Label: "Capital stock", Basis: Form1120BasisClosing},
	{ID: "L22", Section: Form1120SectionScheduleL, Label: "Additional paid-in capital", Basis: Form1120BasisClosing},
	{ID: "L23", Section: Form1120SectionScheduleL, Label: "Retained earnings — appropriated", Basis: Form1120BasisClosing},
	{ID: "L24", Section: Form1120SectionScheduleL, Label: "Retained earnings — unappropriated", Basis: Form1120BasisClosing},
	{ID: "L25", Section: Form1120SectionScheduleL, Label: "Adjustments to shareholders' equity", Basis: Form1120BasisClosing},
	{ID: "L26", Section: Form1120SectionScheduleL, Label: "Less cost of treasury stock", Basis: Form1120BasisClosing},
	{ID: "L27", Section: Form1120SectionScheduleL, Label: "Total liabilities and shareholders' equity", Basis: Form1120BasisClosing,
		Formula: []string{"L16", "L17", "L18", "L19", "L20", "L21", "L22", "L23", "L24", "L25", "L26"}},
	{ID: "LCHECK", Section: Form1120SectionScheduleL, Label: "Difference (line 15 less line 27)", Basis: Form1120BasisClosing,
		Formula: []string{"L15", "-L27"},
		Note:    "A balanced set of books gives zero here. Anything else means an account is missing from the workpaper."},

	// ── Schedule M-1 — income per books reconciled to income per return ────
	{ID: "M1", Section: Form1120SectionScheduleM1, Label: "Net income per books", Basis: Form1120BasisPeriod,
		Note: "The Profit & Loss bottom line for the same period."},
	{ID: "M2", Section: Form1120SectionScheduleM1, Label: "Federal income tax", Basis: Form1120BasisPeriod},
	{ID: "M3", Section: Form1120SectionScheduleM1, Label: "Excess of capital losses over capital gains", Basis: Form1120BasisPeriod},
	{ID: "M4", Section: Form1120SectionScheduleM1, Label: "Income subject to tax not recorded on books this year", Basis: Form1120BasisPeriod},
	{ID: "M5", Section: Form1120SectionScheduleM1, Label: "Expenses recorded on books this year not deducted on this return", Basis: Form1120BasisPeriod},
	{ID: "M6", Section: Form1120SectionScheduleM1, Label: "Total additions", Basis: Form1120BasisPeriod,
		Formula: []string{"M1", "M2", "M3", "M4", "M5"}},
	{ID: "M7", Section: Form1120SectionScheduleM1, Label: "Income recorded on books this year not included on this return", Basis: Form1120BasisPeriod},
	{ID: "M8", Section: Form1120SectionScheduleM1, Label: "Deductions on this return not charged against book income this year", Basis: Form1120BasisPeriod},
	{ID: "M9", Section: Form1120SectionScheduleM1, Label: "Total deductions", Basis: Form1120BasisPeriod,
		Formula: []string{"M7", "M8"}},
	{ID: "M10", Section: Form1120SectionScheduleM1, Label: "Income per return", Basis: Form1120BasisPeriod,
		Formula: []string{"M6", "-M9"},
		Note:    "With no book/tax adjustments recorded, this equals net income per books. Every line 2–8 left blank is an adjustment the accountant still has to make."},
}

// Form1120Mappings places an account on a 1120 line by the account's own Xero
// Type, for the lines a type settles on its own: Xero's SALES type is income
// from any normal business activity, DIRECTCOSTS is the cost of the goods the
// business makes or buys for resale, BANK is the company's cash, INVENTORY is
// the value of goods held for resale, TERMLIAB is borrowings repayable beyond a
// year. A type is a fact about the account that Xero fixes, so these five rules
// hold whatever the organisation's chart is, and none of them names an account
// code or an account name.
//
// Everything else is a placement of a particular account, and Xero's types are
// too coarse to decide it: REVENUE covers Sales, Other Revenue and Interest
// Income alike, and EXPENSE covers every deduction from advertising to wages. So
// those placements are the organisation's own rows in report_line_mappings
// (migration 00028 moved them there out of this file), read by
// form1120Mappings. An accountant corrects one with an UPDATE, and this file
// cannot describe one organisation's chart to another.
//
// A placement pinned to an account beats a type-wide rule, so Sales can be
// pulled out of the REVENUE type it shares with interest and other income.
//
// The rule of thumb is the chart's own: keep a type-wide rule where the type
// settles the line whatever the business does, and leave the rest to the
// organisation's table. The report's "Accounts with no 1120 line" section is its
// own check — an account that lands there is the workpaper saying the return is
// not ready until that account gets a row in report_line_mappings.
var Form1120Mappings = []Form1120Mapping{
	// ── Page 1: income ─────────────────────────────────────────────────────
	{Type: models.AccountTypeSales, Line: "1a",
		Why: "Xero's SALES type is income from any normal business activity — the company's gross receipts."},
	{Type: models.AccountTypeDirectCosts, Line: "2",
		Why: "Xero's DIRECTCOSTS type is the cost of the goods the business makes or buys for resale."},

	// ── Schedule L: assets ─────────────────────────────────────────────────
	{Type: models.AccountTypeBank, Line: "L1",
		Why: "Xero's BANK type is the company's cash."},
	{Type: models.AccountTypeInventory, Line: "L3",
		Why: "Xero's INVENTORY type is the value of goods held for resale."},

	// ── Schedule L: liabilities ────────────────────────────────────────────
	{Type: models.AccountTypeTermLiab, Line: "L19",
		Why: "Xero's TERMLIAB type is borrowings repayable in more than a year."},
}

// Form1120IncomeStatementType reports whether an account type belongs to the
// income statement. Such an account has a movement over the period but no
// balance of its own — the balance sheet carries its effect in retained
// earnings — so the report leaves its closing-balance column blank.
func Form1120IncomeStatementType(accountType string) bool {
	for _, t := range pnlAccountTypes {
		if t == accountType {
			return true
		}
	}
	return false
}
