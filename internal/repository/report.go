package repository

// Reports derived from the GL. Every report is a read-only aggregate over
// `gl_journal_lines` joined to `gl_journals` (for the posting date) and
// `accounts` (for the Chart of Accounts classification).
//
// Rule: every date predicate must actually filter the aggregated lines. A
// bound placed where SELECT/WHERE never references it — the ON-clause of a
// LEFT JOIN whose columns are unused — silently degenerates to "no filter at
// all": the report returns lifetime totals and fromDate/toDate do nothing.
// TrialBalance shares the date-bounded `periodLines` source below; the other
// reports bound `j.journal_date` inline, inside the same query that aggregates.
//
// References:
//   https://developer.xero.com/documentation/api/accounting/reports
//   https://central.xero.com/s/article/Run-the-Profit-and-Loss-report
//   https://central.xero.com/s/article/The-Trial-Balance-report

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

type ReportRepository struct {
	pool *pgxpool.Pool
}

// Period is an inclusive reporting window. Handlers translate Xero's
// `fromDate`/`toDate` query parameters into one; the repository never invents
// a date range of its own.
type Period struct {
	From time.Time
	To   time.Time
}

// periodLines is the date-bounded source of GL lines shared by every report.
// `$1` is the organisation id; the single `%s` is replaced by the placeholder
// holding the caller's cutoff date. Callers LEFT JOIN it so accounts without
// activity still appear with zero balances, exactly as Xero lists them.
const periodLines = `
	SELECT l.account_id, l.net_amount, j.journal_date
	  FROM gl_journal_lines l
	  JOIN gl_journals j ON j.journal_id = l.journal_id
	 WHERE j.organisation_id = $1 AND j.journal_date <= %s`

// periodJoin renders the shared line source with `cutoff` as the placeholder
// holding the scope end date.
func periodJoin(cutoff string) string {
	return strings.Replace(periodLines, "%s", cutoff, 1)
}

// quotedList renders Go string constants as a SQL IN-list so account-type
// literals can never drift from the models constants.
func quotedList(vals ...string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = "'" + v + "'"
	}
	return strings.Join(parts, ",")
}

// zeroPtr returns a fresh pointer to zero. Every comparative accumulator must
// own its variable: pointing two of them at one `decimal.Zero` local makes all
// of them alias the same sum, so each total silently reports the others' value.
func zeroPtr() *decimal.Decimal {
	z := decimal.Zero
	return &z
}

// SplitSigned renders a signed net movement as Xero's Debit/Credit pair: the net
// on the side it falls, zero on the other. It is the half of Xero's Trial
// Balance basis that a two-column sum cannot express — an account did not both
// take in and give up the same money, it moved by its net, so the pair carries
// one figure and never two. Callers that hold a balance rather than a movement
// (the trial balance's balance-sheet measure) split it the same way, so "net on
// the side it falls" is written once for the whole report.
func SplitSigned(net decimal.Decimal) (debit, credit decimal.Decimal) {
	if net.IsNegative() {
		return decimal.Zero, net.Neg()
	}
	return net, decimal.Zero
}

// pnlAccountTypes are the accounts the Profit & Loss report aggregates.
var pnlAccountTypes = []string{
	models.AccountTypeRevenue, models.AccountTypeSales, models.AccountTypeDirectCosts,
	models.AccountTypeExpense, models.AccountTypeOverheads, models.AccountTypeDepreciatn,
	models.AccountTypeWages,
}

// incomeAccountTypes are the profit-and-loss classes a sale is posted to, and
// costAccountTypes the classes a purchase is posted to. Both are subsets of
// pnlAccountTypes: the classes left over are neither.
var (
	incomeAccountTypes = []string{models.AccountTypeRevenue, models.AccountTypeSales}
	costAccountTypes   = []string{
		models.AccountTypeDirectCosts, models.AccountTypeExpense, models.AccountTypeOverheads,
		models.AccountTypeDepreciatn, models.AccountTypeWages,
	}
)

// IsIncomeAccountType reports whether `t` is one of the classes a sale is
// posted to.
func IsIncomeAccountType(t string) bool { return slices.Contains(incomeAccountTypes, t) }

// IsProfitAndLossAccountType reports whether `t` is one of the account classes
// the Profit & Loss statement aggregates. It is the line Xero's Trial Balance
// draws through its own table: a profit-and-loss account is reported at its
// year-to-date movement, every other class at its accumulated balance.
func IsProfitAndLossAccountType(t string) bool { return slices.Contains(pnlAccountTypes, t) }

// FinancialYearStart returns the first day of the financial year containing
// `asOf`, using the organisation's configured financial year end. Defaults to
// a calendar year (1 Jan) when the org has not set one.
func (r *ReportRepository) FinancialYearStart(ctx context.Context, orgID uuid.UUID, asOf time.Time) (time.Time, error) {
	endMonth, endDay := 12, 31
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(NULLIF(financial_year_end_month,0),12),
		        COALESCE(NULLIF(financial_year_end_day,0),31)
		   FROM organisations WHERE organisation_id = $1`, orgID).Scan(&endMonth, &endDay); err != nil {
		return time.Time{}, err
	}
	if endMonth < 1 || endMonth > 12 {
		endMonth = 12
	}
	if endDay < 1 || endDay > 31 {
		endDay = 31
	}
	// The day after the year end is the first day of the financial year.
	start := time.Date(asOf.Year(), time.Month(endMonth), endDay, 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, 1)
	if start.After(asOf) {
		start = start.AddDate(-1, 0, 0)
	}
	return start, nil
}

// TrialBalanceRow: one row per account with period movement (Debit/Credit),
// year-to-date movement (YTDDebit/YTDCredit) and the accumulated balance as at
// the report date (ClosingBalance).
//
// Each amount is *netted per account*: the net sits on the side it falls and the
// other side is zero, so exactly one of Debit/Credit and exactly one of
// YTDDebit/YTDCredit is non-zero for any account that appears.
type TrialBalanceRow struct {
	AccountID   uuid.UUID       `json:"AccountID"`
	AccountCode string          `json:"AccountCode"`
	AccountName string          `json:"AccountName"`
	AccountType string          `json:"AccountType"`
	Debit       decimal.Decimal `json:"Debit"`
	Credit      decimal.Decimal `json:"Credit"`
	YTDDebit    decimal.Decimal `json:"YTDDebit"`
	YTDCredit   decimal.Decimal `json:"YTDCredit"`
	// ClosingBalance is every posting to the account up to and including the
	// report date, signed in Xero's convention (positive = debit). It is the
	// measure Xero's Trial Balance prints for a balance-sheet account, and the
	// only one that can represent an account whose journals all predate the
	// financial year — the year-to-date movement of such an account is zero,
	// and a report that filters on the movement drops the account entirely.
	ClosingBalance decimal.Decimal `json:"ClosingBalance"`
}

// TrialBalance aggregates the period [from, to], year-to-date movement measured
// from the organisation's financial year start through `to`, and the closing
// balance as at `to`. Every window is date-bounded inside the join — see the
// package comment.
//
// Each window is summed *signed*, one net figure per account, and split into the
// Debit/Credit pair in Go. That is the basis Xero's Trial Balance is drawn on,
// and the only basis on which its totals mean anything: a trial balance that
// prints 108,392.54 balances no ledger that closes at 42,595.46. Summing gross
// debits and gross credits into two independent columns instead still balances —
// every posting is one or the other, so a double-entry ledger gives equal totals
// either way — which is what hid the defect: an account that moved both ways read
// as their sum. The reference ledger's Sales is Dr 1,019.95 / Cr 30,559.13; Xero
// prints 29,539.18 and the gross columns printed 31,579.08.
func (r *ReportRepository) TrialBalance(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]TrialBalanceRow, error) {
	ytdFrom, err := r.FinancialYearStart(ctx, orgID, to)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, a.code, a.name, a.type,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $2 AND l.journal_date <= $3
		                          THEN l.net_amount END),0) AS period_net,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $4 AND l.journal_date <= $3
		                          THEN l.net_amount END),0) AS ytd_net,
		        COALESCE(SUM(l.net_amount),0) AS closing
		   FROM accounts a
		   LEFT JOIN (`+periodJoin("$3")+`
		   ) l ON l.account_id = a.account_id
		  WHERE a.organisation_id = $1
		  GROUP BY a.account_id, a.code, a.name, a.type
		  ORDER BY a.code`,
		orgID, from, to, ytdFrom)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrialBalanceRow
	for rows.Next() {
		var row TrialBalanceRow
		// The two signed sums are scanned locally and split below: the pair is
		// derived from the net, so no column can disagree with the one it was
		// derived from.
		var periodNet, ytdNet decimal.Decimal
		if err := rows.Scan(&row.AccountID, &row.AccountCode, &row.AccountName, &row.AccountType,
			&periodNet, &ytdNet, &row.ClosingBalance); err != nil {
			return nil, err
		}
		row.Debit, row.Credit = SplitSigned(periodNet)
		row.YTDDebit, row.YTDCredit = SplitSigned(ytdNet)
		out = append(out, row)
	}
	return out, rows.Err()
}

// PnLRow covers one line of the Profit & Loss statement. `Comparative` is nil
// unless the caller asked for a comparative period.
type PnLRow struct {
	AccountID   uuid.UUID        `json:"AccountID"`
	AccountCode string           `json:"AccountCode"`
	AccountName string           `json:"AccountName"`
	AccountType string           `json:"AccountType"`
	Amount      decimal.Decimal  `json:"Amount"`
	Comparative *decimal.Decimal `json:"Comparative,omitempty"`
}

type PnLReport struct {
	From                 time.Time        `json:"FromDate"`
	To                   time.Time        `json:"ToDate"`
	ComparativeFrom      *time.Time       `json:"ComparativeFromDate,omitempty"`
	ComparativeTo        *time.Time       `json:"ComparativeToDate,omitempty"`
	Income               []PnLRow         `json:"Income"`
	CostOfSales          []PnLRow         `json:"CostOfSales"`
	Expenses             []PnLRow         `json:"Expenses"`
	TotalIncome          decimal.Decimal  `json:"TotalIncome"`
	GrossProfit          decimal.Decimal  `json:"GrossProfit"`
	NetProfit            decimal.Decimal  `json:"NetProfit"`
	TotalCostOfSales     decimal.Decimal  `json:"TotalCostOfSales"`
	TotalExpenses        decimal.Decimal  `json:"TotalExpenses"`
	ComparativeIncome    *decimal.Decimal `json:"ComparativeTotalIncome,omitempty"`
	ComparativeGross     *decimal.Decimal `json:"ComparativeGrossProfit,omitempty"`
	ComparativeNet       *decimal.Decimal `json:"ComparativeNetProfit,omitempty"`
	ComparativeCostSales *decimal.Decimal `json:"ComparativeTotalCostOfSales,omitempty"`
	ComparativeExpenses  *decimal.Decimal `json:"ComparativeTotalExpenses,omitempty"`
}

// ProfitAndLoss aggregates revenue/cost/expense accounts over `p`. When `cmp`
// is non-nil the same accounts are aggregated over that window too, so the
// report can be printed year-on-year.
func (r *ReportRepository) ProfitAndLoss(ctx context.Context, orgID uuid.UUID, p Period, cmp *Period) (*PnLReport, error) {
	// One scan covers both windows: the line source is bounded by the later
	// of the two end dates and per-column CASE expressions split the periods.
	cutoff := p.To
	if cmp != nil && cmp.To.After(cutoff) {
		cutoff = cmp.To
	}
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, a.code, a.name, a.type,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $2 AND l.journal_date <= $3
		                          THEN l.net_amount END),0) AS balance,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $4 AND l.journal_date <= $5
		                          THEN l.net_amount END),0) AS comparative
		   FROM accounts a
		   LEFT JOIN (`+periodJoin("$6")+`
		   ) l ON l.account_id = a.account_id
		  WHERE a.organisation_id = $1
		    AND a.type IN (`+quotedList(pnlAccountTypes...)+`)
		  GROUP BY a.account_id, a.code, a.name, a.type
		  ORDER BY a.type, a.code`,
		orgID, p.From, p.To, cmpFrom(cmp), cmpTo(cmp), cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pnl := &PnLReport{From: p.From, To: p.To}
	if cmp != nil {
		pnl.ComparativeFrom, pnl.ComparativeTo = &cmp.From, &cmp.To
		pnl.ComparativeIncome = zeroPtr()
		pnl.ComparativeGross = zeroPtr()
		pnl.ComparativeNet = zeroPtr()
		pnl.ComparativeCostSales = zeroPtr()
		pnl.ComparativeExpenses = zeroPtr()
	}
	for rows.Next() {
		var row PnLRow
		var comparative decimal.Decimal
		if err := rows.Scan(&row.AccountID, &row.AccountCode, &row.AccountName, &row.AccountType,
			&row.Amount, &comparative); err != nil {
			return nil, err
		}
		// Revenue accounts have credit-normal balances — our net_amount is
		// signed in debit-normal form, so revenue appears negative. Flip it
		// so the report shows positive numbers.
		if row.AccountType == models.AccountTypeRevenue || row.AccountType == models.AccountTypeSales {
			row.Amount = row.Amount.Neg()
			comparative = comparative.Neg()
		}
		if cmp != nil {
			row.Comparative = &comparative
		}
		switch row.AccountType {
		case models.AccountTypeRevenue, models.AccountTypeSales:
			pnl.Income = append(pnl.Income, row)
			pnl.TotalIncome = pnl.TotalIncome.Add(row.Amount)
			if pnl.ComparativeIncome != nil {
				*pnl.ComparativeIncome = pnl.ComparativeIncome.Add(comparative)
			}
		case models.AccountTypeDirectCosts:
			pnl.CostOfSales = append(pnl.CostOfSales, row)
			pnl.TotalCostOfSales = pnl.TotalCostOfSales.Add(row.Amount)
			if pnl.ComparativeCostSales != nil {
				*pnl.ComparativeCostSales = pnl.ComparativeCostSales.Add(comparative)
			}
		default:
			pnl.Expenses = append(pnl.Expenses, row)
			pnl.TotalExpenses = pnl.TotalExpenses.Add(row.Amount)
			if pnl.ComparativeExpenses != nil {
				*pnl.ComparativeExpenses = pnl.ComparativeExpenses.Add(comparative)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pnl.GrossProfit = pnl.TotalIncome.Sub(pnl.TotalCostOfSales)
	pnl.NetProfit = pnl.GrossProfit.Sub(pnl.TotalExpenses)
	if cmp != nil {
		*pnl.ComparativeGross = pnl.ComparativeIncome.Sub(*pnl.ComparativeCostSales)
		*pnl.ComparativeNet = pnl.ComparativeGross.Sub(*pnl.ComparativeExpenses)
	}
	return pnl, nil
}

func cmpFrom(p *Period) time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.From
}

func cmpTo(p *Period) time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.To
}

type BalanceSheetRow struct {
	AccountID   uuid.UUID        `json:"AccountID"`
	AccountCode string           `json:"AccountCode"`
	AccountName string           `json:"AccountName"`
	AccountType string           `json:"AccountType"`
	Amount      decimal.Decimal  `json:"Amount"`
	Comparative *decimal.Decimal `json:"Comparative,omitempty"`
}

type BalanceSheet struct {
	AsOf                        time.Time         `json:"AsOf"`
	ComparativeAsOf             *time.Time        `json:"ComparativeAsOf,omitempty"`
	Assets                      []BalanceSheetRow `json:"Assets"`
	Liabilities                 []BalanceSheetRow `json:"Liabilities"`
	Equity                      []BalanceSheetRow `json:"Equity"`
	TotalAssets                 decimal.Decimal   `json:"TotalAssets"`
	TotalLiabilities            decimal.Decimal   `json:"TotalLiabilities"`
	TotalEquity                 decimal.Decimal   `json:"TotalEquity"`
	RetainedEarnings            decimal.Decimal   `json:"RetainedEarnings"`
	ComparativeTotalAssets      *decimal.Decimal  `json:"ComparativeTotalAssets,omitempty"`
	ComparativeTotalLiabilities *decimal.Decimal  `json:"ComparativeTotalLiabilities,omitempty"`
	ComparativeTotalEquity      *decimal.Decimal  `json:"ComparativeTotalEquity,omitempty"`
	ComparativeRetainedEarnings *decimal.Decimal  `json:"ComparativeRetainedEarnings,omitempty"`
}

// BalanceSheet computes the balance sheet as at `asOf`. When `cmp` is non-nil
// every balance is also computed as at that earlier date — this is what makes
// IRS Schedule L (beginning-of-year vs end-of-year columns) fillable.
func (r *ReportRepository) BalanceSheet(ctx context.Context, orgID uuid.UUID, asOf time.Time, cmp *time.Time) (*BalanceSheet, error) {
	cutoff := asOf
	if cmp != nil && cmp.After(cutoff) {
		cutoff = *cmp
	}
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, a.code, a.name, a.type,
		        COALESCE(SUM(CASE WHEN l.journal_date <= $2 THEN l.net_amount END),0) AS balance,
		        COALESCE(SUM(CASE WHEN l.journal_date <= $3 THEN l.net_amount END),0) AS comparative
		   FROM accounts a
		   LEFT JOIN (`+periodJoin("$4")+`
		   ) l ON l.account_id = a.account_id
		  WHERE a.organisation_id = $1
		  GROUP BY a.account_id, a.code, a.name, a.type
		  ORDER BY a.type, a.code`,
		orgID, asOf, cmpAsOf(cmp), cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bs := &BalanceSheet{AsOf: asOf}
	if cmp != nil {
		bs.ComparativeAsOf = cmp
		bs.ComparativeTotalAssets = zeroPtr()
		bs.ComparativeTotalLiabilities = zeroPtr()
		bs.ComparativeTotalEquity = zeroPtr()
		bs.ComparativeRetainedEarnings = zeroPtr()
	}
	for rows.Next() {
		var row BalanceSheetRow
		var comparative decimal.Decimal
		if err := rows.Scan(&row.AccountID, &row.AccountCode, &row.AccountName, &row.AccountType,
			&row.Amount, &comparative); err != nil {
			return nil, err
		}
		// Liabilities and equity are credit-normal — flip the sign so they
		// print as positives, same as Xero.
		switch row.AccountType {
		case models.AccountTypeCurrLiab, models.AccountTypeLiability,
			models.AccountTypeTermLiab, models.AccountTypePAYGLiab, models.AccountTypeSuperLiab,
			models.AccountTypeEquity:
			row.Amount = row.Amount.Neg()
			comparative = comparative.Neg()
		}
		if cmp != nil {
			row.Comparative = &comparative
		}
		switch row.AccountType {
		case models.AccountTypeBank, models.AccountTypeCurrent, models.AccountTypeFixed,
			models.AccountTypePrepayment, models.AccountTypeInventory, models.AccountTypeNonCurrent:
			bs.Assets = append(bs.Assets, row)
			bs.TotalAssets = bs.TotalAssets.Add(row.Amount)
			if bs.ComparativeTotalAssets != nil {
				*bs.ComparativeTotalAssets = bs.ComparativeTotalAssets.Add(comparative)
			}
		case models.AccountTypeCurrLiab, models.AccountTypeLiability,
			models.AccountTypeTermLiab, models.AccountTypePAYGLiab, models.AccountTypeSuperLiab:
			bs.Liabilities = append(bs.Liabilities, row)
			bs.TotalLiabilities = bs.TotalLiabilities.Add(row.Amount)
			if bs.ComparativeTotalLiabilities != nil {
				*bs.ComparativeTotalLiabilities = bs.ComparativeTotalLiabilities.Add(comparative)
			}
		case models.AccountTypeEquity:
			bs.Equity = append(bs.Equity, row)
			bs.TotalEquity = bs.TotalEquity.Add(row.Amount)
			if bs.ComparativeTotalEquity != nil {
				*bs.ComparativeTotalEquity = bs.ComparativeTotalEquity.Add(comparative)
			}
		case models.AccountTypeRevenue, models.AccountTypeSales,
			models.AccountTypeDirectCosts, models.AccountTypeExpense,
			models.AccountTypeOverheads, models.AccountTypeDepreciatn, models.AccountTypeWages:
			// Net P/L rolls into retained earnings.
			bs.RetainedEarnings = bs.RetainedEarnings.Sub(row.Amount)
			if bs.ComparativeRetainedEarnings != nil {
				*bs.ComparativeRetainedEarnings = bs.ComparativeRetainedEarnings.Sub(comparative)
			}
		}
	}
	bs.TotalEquity = bs.TotalEquity.Add(bs.RetainedEarnings)
	if bs.ComparativeTotalEquity != nil {
		*bs.ComparativeTotalEquity = bs.ComparativeTotalEquity.Add(*bs.ComparativeRetainedEarnings)
	}
	return bs, rows.Err()
}

func cmpAsOf(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

// AgedRow represents one contact in Xero's Aged Receivables Summary / Aged
// Payables Summary. The five amount columns are Xero's five, in Xero's own
// order, and their boundaries are the ones the reference reports measure:
//
//	< 1 Month   0 .. 30 days past due, including anything not yet due
//	1 Month    31 .. 60
//	2 Months   61 .. 90
//	3 Months   91 .. 120
//	Older      more than 120
//
// Note what these are not. Xero's first column has no separate "not yet due"
// bucket: an invoice that is not yet due, or is due on the as-at date, ages into
// `< 1 Month` alongside the one-to-thirty-day ones. That is why the first CASE
// arm is `<= 30` and not `BETWEEN 1 AND 30`. The bound that splits the last two
// columns is 120 days past due, not 90. Both were read off the published column
// totals of docs/xero-reference/aged-receivables-summary.txt and
// aged-payables-summary.txt; docs/xero-aged-parity.md derives them.
type AgedRow struct {
	ContactID      uuid.UUID       `json:"ContactID"`
	ContactName    string          `json:"ContactName"`
	LessThan1Month decimal.Decimal `json:"LessThan1Month"`
	Month1         decimal.Decimal `json:"1Month"`
	Month2         decimal.Decimal `json:"2Months"`
	Month3         decimal.Decimal `json:"3Months"`
	Older          decimal.Decimal `json:"Older"`
	Total          decimal.Decimal `json:"Total"`
}

// ageingBuckets renders the five age columns the aged reports share: how far
// past its due date (falling back to the row's own date) an outstanding amount
// falls. The bands are exhaustive over the integers — <= 30, 31-60, 61-90,
// 91-120, > 120 — so every row lands in exactly one of them and the columns add
// up to the report's total by construction.
//
// `age` is the SQL yielding a row's age in days and `amount` the SQL for its
// outstanding value. With sum true each band is aggregated, which is what the
// by-contact and by-claimant summaries need; with sum false each band is one
// row's own value, which is what the per-invoice drill-down needs. Rendering
// both from one table is what keeps the summary and the drill-down agreeing
// column by column.
func ageingBuckets(age, amount string, sum bool) string {
	bands := []struct{ when, alias string }{
		{"<= 30", "lt1month"},
		{"BETWEEN 31 AND 60", "m1"},
		{"BETWEEN 61 AND 90", "m2"},
		{"BETWEEN 91 AND 120", "m3"},
		{"> 120", "older"},
	}
	cols := make([]string, len(bands))
	for i, b := range bands {
		expr := "CASE WHEN " + age + " " + b.when + " THEN " + amount + " END"
		if sum {
			expr = "SUM(" + expr + ")"
		}
		cols[i] = "COALESCE(" + expr + ",0) AS " + b.alias
	}
	return strings.Join(cols, ",\n\t\t        ")
}

// outstandingInvoices selects the invoices both aged by-contact reports read:
// authorised, still owing something, dated on or before the report date, and
// optionally narrowed to one contact. The summary and the drill-down share it
// so they can never disagree about which invoices the report is about.
const outstandingInvoices = `
		   FROM invoices i JOIN contacts c ON c.contact_id = i.contact_id
		  WHERE i.organisation_id=$1 AND i.type=$2
		    AND i.status='AUTHORISED' AND i.amount_due > 0
		    AND i.date <= $3::date
		    AND ($4::uuid IS NULL OR i.contact_id = $4)`

// Aged buckets every outstanding invoice of `invoiceType` as at `asOf` into
// Xero's five columns by how far past its due date it is (see AgedRow).
// `contactID` narrows the report to a single contact — the shape the two
// `-by-contact` Xero endpoints need.
//
// Bucketing reads COALESCE(due_date, date) so the five buckets partition exactly
// the rows the Total column sums: an invoice with no due date is aged from its
// invoice date (Xero never leaves one out of a bucket), and a row can only be in
// the report at all when its date is non-NULL, so the fallback can never itself
// be NULL.
func (r *ReportRepository) Aged(ctx context.Context, orgID uuid.UUID, invoiceType string, asOf time.Time, contactID *uuid.UUID) ([]AgedRow, error) {
	q := `SELECT c.contact_id, c.name,
		        ` + ageingBuckets("$3::date - COALESCE(i.due_date, i.date)", "i.amount_due", true) + `,
		        COALESCE(SUM(i.amount_due),0) AS total` + outstandingInvoices + `
		  GROUP BY c.contact_id, c.name
		  ORDER BY c.name`
	rows, err := r.pool.Query(ctx, q, orgID, invoiceType, asOf, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgedRow
	for rows.Next() {
		var row AgedRow
		if err := rows.Scan(&row.ContactID, &row.ContactName,
			&row.LessThan1Month, &row.Month1, &row.Month2, &row.Month3, &row.Older,
			&row.Total); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// AgedDetailRow is one outstanding invoice on the Aged <Type> by Contact
// report. The five bucket columns use the same rule and the same boundaries the
// summary report uses (see AgedRow), so exactly one of them is non-zero for a
// given invoice and the two reports agree column by column.
type AgedDetailRow struct {
	ContactID      uuid.UUID       `json:"ContactID"`
	ContactName    string          `json:"ContactName"`
	InvoiceNumber  string          `json:"InvoiceNumber,omitempty"`
	Date           time.Time       `json:"Date"`
	DueDate        *time.Time      `json:"DueDate,omitempty"`
	LessThan1Month decimal.Decimal `json:"LessThan1Month"`
	Month1         decimal.Decimal `json:"1Month"`
	Month2         decimal.Decimal `json:"2Months"`
	Month3         decimal.Decimal `json:"3Months"`
	Older          decimal.Decimal `json:"Older"`
	Total          decimal.Decimal `json:"Total"`
}

// AgedByContact is the drill-down behind the aged summary: the same outstanding
// invoices, one row each instead of one row per contact, ordered by contact so
// a renderer can block them. It reads the same rows, the same status and the
// same age buckets as Aged — only the grouping differs.
func (r *ReportRepository) AgedByContact(ctx context.Context, orgID uuid.UUID, invoiceType string, asOf time.Time, contactID *uuid.UUID) ([]AgedDetailRow, error) {
	q := `SELECT c.contact_id, c.name, COALESCE(i.invoice_number,''), i.date, i.due_date,
		        ` + ageingBuckets("$3::date - COALESCE(i.due_date, i.date)", "i.amount_due", false) + `,
		        i.amount_due` + outstandingInvoices + `
		  ORDER BY c.name, COALESCE(i.due_date, i.date), i.invoice_number`
	rows, err := r.pool.Query(ctx, q, orgID, invoiceType, asOf, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgedDetailRow
	for rows.Next() {
		var row AgedDetailRow
		if err := rows.Scan(&row.ContactID, &row.ContactName, &row.InvoiceNumber, &row.Date, &row.DueDate,
			&row.LessThan1Month, &row.Month1, &row.Month2, &row.Month3, &row.Older,
			&row.Total); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// AgedClaimRow is one claimant in the Expense Claims block of Xero's Aged
// Payables Summary. Xero prints that block between the trade-payables rows and
// the report's overall total, because an unpaid expense claim is a payable too:
// the liability sits in the organisation's Unpaid Expense Claims account until
// the claim is reimbursed. The five amount columns are the same five as AgedRow,
// measured from the claim's own date.
//
// The claimant is the user the claim is recorded against — Xero's Aged Payables
// Summary names the person, and `expense_claims.user_id` is the only claimant
// record the schema keeps. When a claim has no user at all the row is labelled
// with ClaimantUnassigned rather than with a name taken from somewhere else.
type AgedClaimRow struct {
	ClaimantName   string
	LessThan1Month decimal.Decimal
	Month1         decimal.Decimal
	Month2         decimal.Decimal
	Month3         decimal.Decimal
	Older          decimal.Decimal
	Total          decimal.Decimal
}

// ClaimantUnassigned labels an unpaid expense claim that is recorded against no
// user. It is a statement that the claimant is not recorded, not a name.
const ClaimantUnassigned = "Unassigned expense claim"

// AgedExpenseClaims buckets the organisation's unpaid expense claims by
// claimant, for the Expense Claims block of the Aged Payables Summary.
//
// "Unpaid" is the same test Aged applies to invoices — the claim still owes
// something and has not been paid or deleted (`amount_due > 0`, status
// SUBMITTED or AUTHORISED) — and the claim is dated the way an invoice is:
// COALESCE(payment_due_date, reporting_date), so a claim with a payment due date
// is aged from it and one without is aged from the date it was reported. A claim
// with neither date is left out: it cannot be placed in an as-at report at all,
// and inventing a date for it would put an amount in a column nothing supports.
//
// The label comes from the linked user (`users.first_name` + `last_name`), or the
// user's email when they have no name recorded, and only ever falls back to
// ClaimantUnassigned when there is no user: the report never prints a name that
// is not in the database.
func (r *ReportRepository) AgedExpenseClaims(ctx context.Context, orgID uuid.UUID, asOf time.Time) ([]AgedClaimRow, error) {
	q := `SELECT COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), ''), u.email, $3) AS claimant,
		        ` + ageingBuckets("$2::date - COALESCE(ec.payment_due_date, ec.reporting_date)", "ec.amount_due", true) + `,
		        COALESCE(SUM(ec.amount_due),0) AS total
		   FROM expense_claims ec
		   LEFT JOIN users u ON u.user_id = ec.user_id
		  WHERE ec.organisation_id=$1
		    AND ec.status IN ('SUBMITTED','AUTHORISED')
		    AND ec.amount_due > 0
		    AND COALESCE(ec.payment_due_date, ec.reporting_date) <= $2::date
		  GROUP BY u.user_id, u.first_name, u.last_name, u.email
		  ORDER BY claimant`
	rows, err := r.pool.Query(ctx, q, orgID, asOf, ClaimantUnassigned)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgedClaimRow
	for rows.Next() {
		var row AgedClaimRow
		if err := rows.Scan(&row.ClaimantName,
			&row.LessThan1Month, &row.Month1, &row.Month2, &row.Month3, &row.Older,
			&row.Total); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// CashSummary's sections. A row lands in one of them by the role the account
// plays in the period's cash movement, which is a different question from the
// account's own class — see cashSectionFor.
const (
	CashSectionIncome  = "income"
	CashSectionExpense = "expense"
	CashSectionOther   = "other"
)

// cashSectionFor names the section an account of this class prints in: an
// income account is Income, a cost account Less Expenses, and every other
// class — the balance-sheet accounts the money also moved to and from — is a
// cash movement of its own, the report's Plus Other Cash Movements.
//
// No account is named. 840 Historical Adjustment is a current liability in this
// chart and prints in Plus Other Cash Movements for that reason and no other;
// Xero's own capture prints it inside Less Expenses (docs/xero-reference/
// cash-summary.txt line 20) while both its Balance Sheet (line 25) and its Trial
// Balance file the account under Current Liabilities, and this report follows
// the account's own class rather than carrying a list of accounts to except.
func cashSectionFor(accountType string) string {
	if IsIncomeAccountType(accountType) {
		return CashSectionIncome
	}
	if slices.Contains(costAccountTypes, accountType) {
		return CashSectionExpense
	}
	return CashSectionOther
}

// CashCategoryRow is one account's contribution to the period's cash movement:
// the amount that account's coding put into (+) or took out of (−) the bank
// accounts. It is not the account's own movement in the general ledger — where
// the cash was coded to a control account the report looks through to the
// document that explains it — which is what makes a cash summary explain the
// movement rather than merely restate the bank balance.
type CashCategoryRow struct {
	AccountID   uuid.UUID `json:"AccountID"`
	AccountCode string    `json:"AccountCode"`
	AccountName string    `json:"AccountName"`
	AccountType string    `json:"AccountType"`
	// Section is the part of the report the row prints in, one of the
	// CashSection values above.
	Section string          `json:"Section"`
	Amount  decimal.Decimal `json:"Amount"`
	// Tax is the tax recorded on the same attributed lines, carried in the same
	// cash direction as Amount: an expense row of −46.19 carries −3.81. It is
	// what the report's Plus Tax Movements section is made of, and Amount is
	// the net of it.
	Tax decimal.Decimal `json:"Tax"`
	// Unattributed marks the part of a control account's own cash that this
	// ledger links to no document at all. It is printed as the account the
	// journal itself named, never spread over the accounts a document might
	// have coded it to, and the report states its total in the titles.
	Unattributed bool `json:"Unattributed,omitempty"`
}

// CashSummaryReport is the whole Cash Summary: the period's cash movement
// attributed to the accounts the money moved to and from, the tax that movement
// carried, and the bank balances either side of the window.
type CashSummaryReport struct {
	Categories []CashCategoryRow
	// CreditNoteTax is the tax on the credit notes the period's cash settled
	// with. Those notes' own lines are subtracted from the accounts they name,
	// tax included, so their tax is added back here for the Plus Tax Movements
	// section to carry: the same amount added to Tax Collected and taken off
	// Tax Paid leaves the net between them unchanged either way.
	CreditNoteTax decimal.Decimal
	// TaxCollected and TaxPaid are the two halves of the period's tax: the tax
	// the attributed lines on income accounts carry, and the tax every other
	// attributed line carries, each with the credit-note tax above. They are the
	// figures the renderer prints and Net Tax Movements is their sum.
	TaxCollected decimal.Decimal
	TaxPaid      decimal.Decimal
	// OtherMovement is what the report's Plus Other Cash Movements section
	// contributes to the movement. It is the sum of that section's rows: the
	// renderer prints those rows and sums them itself, and this field is what a
	// caller (or a test) checks that sum against.
	OtherMovement  decimal.Decimal
	OpeningBalance decimal.Decimal
	ClosingBalance decimal.Decimal
	// NetMovement is measured on the bank accounts themselves (closing minus
	// opening). It is the figure the rendered rows must add up to: the renderer
	// derives its Net Cash Movement row from those rows, and this field is what
	// a caller (or a test) checks that derivation against.
	NetMovement decimal.Decimal
}

// cashControls names the four control accounts the Cash Summary classifies
// through: the two document control accounts, the expense-claim control account,
// and the tax account a journal posts to when it records a tax a second time.
//
// They are the organisation's own accounts, resolved by the Xero system role
// each one holds (Account.SystemAccount -- models/account.go lists the strings
// with Xero's own spelling and migration 00027 declares this chart's), so no
// account code is written here. A chart whose Accounts Receivable is 120 rather
// than 610, or whose sales tax account is 220 rather than 820, is read exactly
// the same way; that is the point, because the code is the organisation's own
// label and the role is the fact.
//
// A role the organisation has not declared resolves to the empty string, which
// no account code equals. That is deliberate, and it is the calling query's own
// behaviour rather than a special case here: an undeclared role cannot be
// classified, so its cash is left unclassified rather than attributed to
// whichever account happened to hold a hardcoded code.
type cashControlCodes struct {
	receivable string
	payable    string
	claims     string
	tax        string
}

func (r *ReportRepository) cashControls(ctx context.Context, orgID uuid.UUID) (cashControlCodes, error) {
	var out cashControlCodes
	rows, err := r.pool.Query(ctx,
		`SELECT system_account, code FROM accounts
		  WHERE organisation_id = $1 AND system_account IN ($2,$3,$4,$5)
		  ORDER BY code`,
		orgID,
		models.SystemAccountDebtors, models.SystemAccountCreditors,
		models.SystemAccountUnpaidExpClm, models.SystemAccountGST)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var role, code string
		if err := rows.Scan(&role, &code); err != nil {
			return out, err
		}
		// ORDER BY code fixes which account wins if a chart tags one role on
		// more than one account, so the report cannot change its mind between
		// two runs over the same data.
		switch role {
		case models.SystemAccountDebtors:
			if out.receivable == "" {
				out.receivable = code
			}
		case models.SystemAccountCreditors:
			if out.payable == "" {
				out.payable = code
			}
		case models.SystemAccountUnpaidExpClm:
			if out.claims == "" {
				out.claims = code
			}
		case models.SystemAccountGST:
			if out.tax == "" {
				out.tax = code
			}
		}
	}
	return out, rows.Err()
}

// CashSummary aggregates the period from the GL rows that moved cash: where the
// money was coded, looking through the Accounts Receivable, Accounts Payable and
// Unpaid Expense Claims control accounts to the documents the cash settled, and
// the bank balances either side of the window.
func (r *ReportRepository) CashSummary(ctx context.Context, orgID uuid.UUID, from, to time.Time) (*CashSummaryReport, error) {
	out := &CashSummaryReport{}

	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(CASE WHEN j.journal_date < $2 THEN l.net_amount END),0),
		        COALESCE(SUM(l.net_amount),0)
		   FROM gl_journal_lines l
		   JOIN gl_journals j ON j.journal_id = l.journal_id
		   JOIN accounts a ON a.account_id = l.account_id
		  WHERE j.organisation_id = $1 AND a.type = $4
		    AND j.journal_date <= $3`,
		orgID, from, to, models.AccountTypeBank).
		Scan(&out.OpeningBalance, &out.ClosingBalance); err != nil {
		return nil, err
	}
	out.NetMovement = out.ClosingBalance.Sub(out.OpeningBalance)

	// Where the money was coded. On the cash-basis view the counterpart of a
	// bank journal is not the account the journal landed on but the account the
	// document behind it coded the money to, so the two control accounts are
	// read off the journal's own lines and looked through: a receipt is
	// attributed to the invoice's line items in proportion to the cash applied
	// to it, a supplier payment to the bill's, and an expense-claim payment to
	// the claim journal's coding. What the ledger links no document to is not
	// guessed at — it stays on the account the journal itself named, marked
	// unattributed, and the control account's residual below is what keeps the
	// report footing when a link is missing.
	//
	// The tax the ledger records beside a line's net is carried on the row
	// rather than looked for in a tax account, and where the same journal posts
	// that tax a second time as a line on 820 Sales Tax the second posting is
	// left out — it is the same tax, and counting both would double it.
	controls, err := r.cashControls(ctx, orgID)
	if err != nil {
		return nil, err
	}
	cats, err := r.pool.Query(ctx,
		`WITH bank AS (
		    SELECT j.journal_id,
		           (SELECT a2.code FROM gl_journal_lines l2
		              JOIN accounts a2 ON a2.account_id = l2.account_id
		             WHERE l2.journal_id = j.journal_id AND a2.code IN ($5,$6,$7)
		             ORDER BY a2.code LIMIT 1) AS control
		      FROM gl_journals j
		     WHERE j.organisation_id = $1
		       AND j.journal_date BETWEEN $2 AND $3
		       AND EXISTS (SELECT 1 FROM gl_journal_lines bl
		                     JOIN accounts ba ON ba.account_id = bl.account_id
		                    WHERE bl.journal_id = j.journal_id AND ba.type = $4)
		),
		-- What each control account's own cash came to, measured on the bank
		-- side of the journals that used it so that every cash-moving journal is
		-- counted exactly once.
		journal_cash AS (
		    SELECT b.control, SUM(l.net_amount) AS cash
		      FROM bank b
		      JOIN gl_journal_lines l ON l.journal_id = b.journal_id
		      JOIN accounts a ON a.account_id = l.account_id
		     WHERE b.control IS NOT NULL AND a.type = $4
		     GROUP BY b.control
		),
		-- The cash each document received in the period, and the share of the
		-- document that cash explains. An allocated credit note is part of the
		-- settlement, so the credit note's own coding is subtracted from the
		-- accounts it names further down.
		doc_pay AS (
		    SELECT p.payment_type, p.amount, i.invoice_id, i.type AS doc_type,
		           i.line_amount_types,
		           LEAST(1, (p.amount + COALESCE(ca.amount,0)) / NULLIF(i.total,0)) AS frac
		      FROM payments p
		      JOIN invoices i ON i.invoice_id = p.invoice_id
		      LEFT JOIN (SELECT ca.invoice_id, SUM(ca.amount) AS amount
		                   FROM credit_note_allocations ca GROUP BY ca.invoice_id) ca
		        ON ca.invoice_id = p.invoice_id
		     WHERE p.organisation_id = $1 AND p.date BETWEEN $2 AND $3
		),
		-- A line's coding is its account code; the id beside it is populated by
		-- the import but not by every path that writes a line, so the code is
		-- what the row is resolved by. An inclusive document's line_amount is
		-- already gross, so tax comes off it here; an exclusive line's is net and
		-- tax is added back at the end. Either way amount + tax is the cash.
		doc_attr AS (
		    SELECT CASE WHEN dp.doc_type = 'ACCPAY' THEN $6 ELSE $5 END AS control,
		           COALESCE(li.account_id, ac.account_id) AS account_id,
		           (CASE WHEN dp.doc_type = 'ACCPAY' THEN -1 ELSE 1 END)
		             * ROUND((CASE WHEN dp.line_amount_types = 'Inclusive'
		                           THEN li.line_amount - li.tax_amount ELSE li.line_amount END) * dp.frac, 2) AS amount,
		           (CASE WHEN dp.doc_type = 'ACCPAY' THEN -1 ELSE 1 END)
		             * ROUND(li.tax_amount * dp.frac, 2) AS tax
		      FROM doc_pay dp
		      JOIN invoice_line_items li ON li.invoice_id = dp.invoice_id
		      LEFT JOIN accounts ac ON ac.organisation_id = $1 AND ac.code = li.account_code
		),
		cn_attr AS (
		    SELECT CASE WHEN dp.doc_type = 'ACCPAY' THEN $6 ELSE $5 END AS control,
		           a.account_id,
		           (CASE WHEN dp.doc_type = 'ACCPAY' THEN 1 ELSE -1 END)
		             * ROUND((CASE WHEN cn.line_amount_types = 'Inclusive'
		                           THEN li.line_amount - li.tax_amount ELSE li.line_amount END)
		                     * ca.amount / NULLIF(cn.total,0), 2) AS amount,
		           (CASE WHEN dp.doc_type = 'ACCPAY' THEN 1 ELSE -1 END)
		             * ROUND(li.tax_amount * ca.amount / NULLIF(cn.total,0), 2) AS tax
		      FROM credit_note_allocations ca
		      JOIN credit_notes cn ON cn.credit_note_id = ca.credit_note_id
		      JOIN doc_pay dp ON dp.invoice_id = ca.invoice_id
		      JOIN credit_note_line_items li ON li.credit_note_id = cn.credit_note_id
		      JOIN accounts a ON a.organisation_id = cn.organisation_id AND a.code = li.account_code
		),
		-- An expense claim is coded on its own journal, not on the bank journal
		-- that paid it; the two are linked by the amount on 801, because they
		-- are not dated the same day.
		claim_attr AS (
		    SELECT $7 AS control, l.account_id, -l.net_amount AS amount,
		           CASE WHEN $8 <> '' THEN -l.tax_amount ELSE 0 END AS tax
		      FROM bank b
		      JOIN gl_journal_lines jl ON jl.journal_id = b.journal_id
		      JOIN accounts ctl ON ctl.account_id = jl.account_id AND ctl.code = $7
		      JOIN gl_journals cj ON cj.organisation_id = $1 AND cj.source_type = 'EXPENSECLAIM'
		                         AND EXISTS (SELECT 1 FROM gl_journal_lines cl
		                                       JOIN accounts ca2 ON ca2.account_id = cl.account_id
		                                      WHERE cl.journal_id = cj.journal_id AND ca2.code = $7
		                                        AND cl.net_amount = -jl.net_amount)
		      JOIN gl_journal_lines l ON l.journal_id = cj.journal_id
		      JOIN accounts a ON a.account_id = l.account_id
		     WHERE b.control = $7 AND a.code NOT IN ($7,$8)
		),
		-- Everything a bank journal coded outside a control account and outside
		-- the tax account: the direct spends and receipts, and the manual
		-- journals over a bank account. When the chart has no sales-tax system
		-- account the posting folded tax into net_amount (gl.go), so net is
		-- already gross there and the tax column must stay empty — adding it
		-- again would count the same tax twice.
		direct_attr AS (
		    SELECT b.control, l.account_id, -l.net_amount AS amount,
		           CASE WHEN $8 <> '' THEN -l.tax_amount ELSE 0 END AS tax
		      FROM bank b
		      JOIN gl_journal_lines l ON l.journal_id = b.journal_id
		      JOIN accounts a ON a.account_id = l.account_id
		     WHERE a.type <> $4 AND a.code <> $8 AND a.code IS DISTINCT FROM b.control
		),
		all_attr AS (
		    SELECT * FROM doc_attr UNION ALL SELECT * FROM cn_attr
		    UNION ALL SELECT * FROM claim_attr UNION ALL SELECT * FROM direct_attr
		),
		attributed AS (
		    SELECT control, SUM(amount + tax) AS cash FROM all_attr
		     WHERE control IS NOT NULL GROUP BY control
		),
		-- A document link this ledger does not hold: the control account's cash
		-- that no document accounts for.
		residual AS (
		    SELECT jc.control, jc.cash - COALESCE(at.cash, 0) AS cash
		      FROM journal_cash jc
		      LEFT JOIN attributed at ON at.control = jc.control
		     WHERE jc.cash - COALESCE(at.cash, 0) <> 0
		)
		SELECT a.account_id, a.code, a.name, a.type, r.amount, r.tax, r.unattributed
		  FROM (
		        SELECT account_id, SUM(amount) AS amount, SUM(tax) AS tax, false AS unattributed
		          FROM all_attr GROUP BY account_id
		        UNION ALL
		        SELECT ra.account_id, rs.cash, 0, true
		          FROM residual rs
		          JOIN accounts ra ON ra.organisation_id = $1 AND ra.code = rs.control
		       ) r
		  JOIN accounts a ON a.account_id = r.account_id
		 ORDER BY a.code, r.unattributed`,
		orgID, from, to, models.AccountTypeBank,
		controls.receivable, controls.payable, controls.claims, controls.tax)
	if err != nil {
		return nil, err
	}
	defer cats.Close()
	for cats.Next() {
		var row CashCategoryRow
		if err := cats.Scan(&row.AccountID, &row.AccountCode, &row.AccountName,
			&row.AccountType, &row.Amount, &row.Tax, &row.Unattributed); err != nil {
			return nil, err
		}
		row.Section = cashSectionFor(row.AccountType)
		out.Categories = append(out.Categories, row)
		switch row.Section {
		case CashSectionIncome:
			out.TaxCollected = out.TaxCollected.Add(row.Tax)
		case CashSectionOther:
			out.OtherMovement = out.OtherMovement.Add(row.Amount)
			out.TaxPaid = out.TaxPaid.Add(row.Tax)
		default:
			out.TaxPaid = out.TaxPaid.Add(row.Tax)
		}
	}
	if err := cats.Err(); err != nil {
		return nil, err
	}

	// The tax on the credit notes the ledger has allocated, added back against
	// the tax their own lines took off the accounts above. It is taken over
	// every allocation rather than only those whose document took cash in the
	// window: a note's tax is the same tax whenever the cash that settled it
	// moved, and the amount is added to Tax Collected and taken off Tax Paid, so
	// whichever reading is used the net between them is unchanged.
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(cn.total_tax),0)
		   FROM credit_note_allocations ca
		   JOIN credit_notes cn ON cn.credit_note_id = ca.credit_note_id
		  WHERE cn.organisation_id = $1`,
		orgID).Scan(&out.CreditNoteTax); err != nil {
		return nil, err
	}
	out.TaxCollected = out.TaxCollected.Add(out.CreditNoteTax)
	out.TaxPaid = out.TaxPaid.Sub(out.CreditNoteTax)
	return out, nil
}

// SalesTaxRow powers the BAS / sales-tax report: one row per tax rate the
// organisation's own coded journals carry, with the net sales and net purchases
// posted under it.
//
// The net columns are profit-and-loss columns: Net Sales sums the lines that
// hit an income account and Net Purchases the lines that hit a cost account,
// and a line on any other class — an asset, a liability, equity — is neither.
// The tax columns are a tax workpaper and cannot draw that line: Tax Collected
// sums every coded line on an income account and Tax Paid every coded line that
// is not one, including the capital lines on FIXED accounts, whose input tax is
// real tax on a real purchase. Between them the two tax columns account for
// every line of the period that names a rate.
type SalesTaxRow struct {
	TaxType string `json:"TaxType"`
	// TaxRateName is the rate's name from the organisation's tax-rate table.
	// It is empty when the journals name a rate the organisation has not
	// defined, in which case the report falls back to the tax type itself.
	TaxRateName  string          `json:"TaxRateName,omitempty"`
	NetSales     decimal.Decimal `json:"NetSales"`
	NetPurchases decimal.Decimal `json:"NetPurchases"`
	// TaxCollected and TaxPaid are the whole of the tax the ledger records on
	// the rate's own lines — every coded line on an income account for the one,
	// every coded line that is not on one for the other. A line whose tax is
	// not determined records no tax at all, so it adds nothing to these sums.
	// They are therefore complete as the sum of recorded tax; what the counts
	// below are for is saying how many lines the ledger records no tax for.
	TaxCollected decimal.Decimal `json:"TaxCollected"`
	TaxPaid      decimal.Decimal `json:"TaxPaid"`
	// SalesLines and PurchaseLines count the lines each side of the workpaper
	// holds, and MeasuredSales and MeasuredPurchases how many of those record a
	// tax the report can stand behind. The difference is the ledger's own hole,
	// not the report's: the sums above are the whole of the tax the ledger
	// records, and these counts are what the report states in its own body so a
	// reader knows how much of the column the ledger holds no tax for.
	SalesLines        int64 `json:"SalesLines"`
	PurchaseLines     int64 `json:"PurchaseLines"`
	MeasuredSales     int64 `json:"MeasuredSales"`
	MeasuredPurchases int64 `json:"MeasuredPurchases"`
}

// SalesTaxByRate aggregates the coded journal lines by tax type.
//
// The row set comes from the lines, not from the tax-rate table: a rate the
// organisation defines but has never posted under is not a row, and a rate it
// has posted under is — so nothing here depends on a list of known rates.
// Sales and purchases are told apart by the class of the account each line hit
// (income classes are sales, cost and expense classes are purchases); a line on
// any other class — an asset, a liability, equity — is not a sale or a purchase
// and contributes to neither net column.
//
// The tax columns are not split the same way. Tax Collected counts the lines on
// income accounts and Tax Paid every coded line that is not on one, whatever
// its class: an input tax posted to a FIXED account is tax on a purchase like
// any other, and a workpaper that dropped it would overstate the liability.
//
// A line's tax is determined when the ledger records one — the document behind
// it supplied a figure, or the rate it names is 0%, in which case zero is the
// answer rather than a hole. A rate whose rate table row is missing is not
// assumed to be 0%, so its zero lines count as undetermined. An undetermined
// line records 0.00, and that 0.00 goes into the sums below like any other: the
// report prints the whole of what the ledger records and states how many lines
// it records nothing for, rather than dropping a rate's entire column over a
// line that contributes nothing to it.
func (r *ReportRepository) SalesTaxByRate(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]SalesTaxRow, error) {
	rows, err := r.pool.Query(ctx, `
		WITH rates AS (
			SELECT DISTINCT ON (tr.tax_type) tr.tax_type,
			       (COALESCE(tr.effective_rate, 0) = 0) AS zero_rated
			  FROM tax_rates tr
			 WHERE tr.organisation_id = $1
			 ORDER BY tr.tax_type, tr.name
		),
		coded AS (
			SELECT l.tax_type,
			       a.type IN (`+quotedList(incomeAccountTypes...)+`) AS is_income,
			       a.type IN (`+quotedList(costAccountTypes...)+`) AS is_cost,
			       l.net_amount,
			       l.tax_amount,
			       (l.tax_amount <> 0 OR COALESCE(rt.zero_rated, false)) AS determined
			  FROM gl_journal_lines l
			  JOIN gl_journals j ON j.journal_id = l.journal_id
			  JOIN accounts a ON a.account_id = l.account_id
			  LEFT JOIN rates rt ON rt.tax_type = l.tax_type
			 WHERE j.organisation_id = $1
			   AND j.journal_date BETWEEN $2 AND $3
			   AND COALESCE(l.tax_type,'') <> ''
		),
		agg AS (
			SELECT tax_type,
			       COALESCE(SUM(CASE WHEN is_income THEN -net_amount END),0) AS net_sales,
			       COALESCE(SUM(CASE WHEN is_cost THEN net_amount END),0) AS net_purchases,
			       COALESCE(SUM(CASE WHEN is_income THEN tax_amount END),0) AS tax_collected,
			       COALESCE(SUM(CASE WHEN NOT is_income THEN tax_amount END),0) AS tax_paid,
			       COUNT(*) FILTER (WHERE is_income) AS sales_lines,
			       COUNT(*) FILTER (WHERE NOT is_income) AS purchase_lines,
			       COUNT(*) FILTER (WHERE is_income AND determined) AS measured_sales,
			       COUNT(*) FILTER (WHERE NOT is_income AND determined) AS measured_purchases
			  FROM coded
			 GROUP BY tax_type
		)
		SELECT agg.tax_type,
		       COALESCE((SELECT tr.name FROM tax_rates tr
		                  WHERE tr.organisation_id = $1 AND tr.tax_type = agg.tax_type
		                  ORDER BY tr.name LIMIT 1),'') AS tax_rate_name,
		       agg.net_sales, agg.net_purchases, agg.tax_collected, agg.tax_paid,
		       agg.sales_lines, agg.purchase_lines,
		       agg.measured_sales, agg.measured_purchases
		  FROM agg
		 ORDER BY agg.tax_type`,
		orgID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SalesTaxRow
	for rows.Next() {
		var row SalesTaxRow
		if err := rows.Scan(&row.TaxType, &row.TaxRateName, &row.NetSales, &row.NetPurchases,
			&row.TaxCollected, &row.TaxPaid, &row.SalesLines, &row.PurchaseLines,
			&row.MeasuredSales, &row.MeasuredPurchases); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// JournalFeedRow is one row per GL posting for the Journal report and for the
// Account Transactions / General Ledger Detail reports.
type JournalFeedRow struct {
	Date        time.Time       `json:"Date"`
	Source      string          `json:"Source"`
	Reference   string          `json:"Reference"`
	AccountID   uuid.UUID       `json:"AccountID"`
	AccountCode string          `json:"AccountCode"`
	AccountName string          `json:"AccountName"`
	Description string          `json:"Description,omitempty"`
	Debit       decimal.Decimal `json:"Debit"`
	Credit      decimal.Decimal `json:"Credit"`
}

// journalFeed lists GL lines between `from` and `to`, optionally narrowed to
// `accountIDs` (all accounts when the slice is empty). Ordering is by account
// then date, which is what both drill-down reports need, and then by line_id --
// the key without which the order is not total. Two lines of the same journal
// that post to the same account on the same date tie on the first three columns,
// and the heap decides the rest, so General Ledger Detail's running Balance
// column would print different figures for the same row on two calls of the same
// URL. line_id is the primary key, so adding it makes the order total and the
// report reproducible.
func (r *ReportRepository) journalFeed(ctx context.Context, orgID uuid.UUID, from, to time.Time, accountIDs []uuid.UUID) ([]JournalFeedRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT j.journal_date, j.source_type, COALESCE(j.reference,''),
		       a.account_id, a.code, a.name, COALESCE(l.description,''),
		       CASE WHEN l.net_amount > 0 THEN  l.net_amount ELSE 0 END AS debit,
		       CASE WHEN l.net_amount < 0 THEN -l.net_amount ELSE 0 END AS credit
		  FROM gl_journals j
		  JOIN gl_journal_lines l ON l.journal_id = j.journal_id
		  JOIN accounts a         ON a.account_id = l.account_id
		 WHERE j.organisation_id=$1 AND j.journal_date BETWEEN $2 AND $3
		   AND (COALESCE(array_length($4::uuid[],1),0) = 0 OR a.account_id = ANY($4::uuid[]))
		 ORDER BY a.code, j.journal_date, j.journal_id, l.line_id`,
		orgID, from, to, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalFeedRow
	for rows.Next() {
		var row JournalFeedRow
		if err := rows.Scan(&row.Date, &row.Source, &row.Reference,
			&row.AccountID, &row.AccountCode, &row.AccountName, &row.Description,
			&row.Debit, &row.Credit); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// JournalFeed returns every GL line between `from` and `to` for the Journal
// report handler.
func (r *ReportRepository) JournalFeed(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]JournalFeedRow, error) {
	return r.journalFeed(ctx, orgID, from, to, nil)
}

// AccountTransactions returns the GL lines posted to `accountIDs` (all accounts
// when the slice is empty) over the period, in account + date order. It powers
// the "Account Transactions" report and the drill-down from every other report.
func (r *ReportRepository) AccountTransactions(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID, from, to time.Time) ([]JournalFeedRow, error) {
	return r.journalFeed(ctx, orgID, from, to, accountIDs)
}

// GLLine is one posted line inside a General Ledger account group.
type GLLine struct {
	Date        time.Time       `json:"Date"`
	Source      string          `json:"Source"`
	Reference   string          `json:"Reference"`
	Description string          `json:"Description,omitempty"`
	Debit       decimal.Decimal `json:"Debit"`
	Credit      decimal.Decimal `json:"Credit"`
	Balance     decimal.Decimal `json:"Balance"`
}

// GLAccountGroup is one account block of the General Ledger Detail report:
// opening balance, the posted lines with a running balance, and the closing
// balance. This is the drill-down an accountant uses to build Form 1120.
type GLAccountGroup struct {
	AccountID   uuid.UUID       `json:"AccountID"`
	AccountCode string          `json:"AccountCode"`
	AccountName string          `json:"AccountName"`
	AccountType string          `json:"AccountType"`
	Opening     decimal.Decimal `json:"OpeningBalance"`
	Debit       decimal.Decimal `json:"Debit"`
	Credit      decimal.Decimal `json:"Credit"`
	Closing     decimal.Decimal `json:"ClosingBalance"`
	Lines       []GLLine        `json:"Lines"`
}

// GeneralLedgerDetail builds one group per account (including accounts with no
// movement, mirroring Xero) with the opening balance as at `from` exclusive.
func (r *ReportRepository) GeneralLedgerDetail(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]GLAccountGroup, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, a.code, a.name, a.type,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $2 AND l.journal_date <= $3 AND l.net_amount > 0
		                          THEN  l.net_amount END),0) AS debit,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $2 AND l.journal_date <= $3 AND l.net_amount < 0
		                          THEN -l.net_amount END),0) AS credit,
		        COALESCE(SUM(CASE WHEN l.journal_date <= $3 THEN l.net_amount END),0) AS closing
		   FROM accounts a
		   LEFT JOIN (`+periodJoin("$3")+`
		   ) l ON l.account_id = a.account_id
		  WHERE a.organisation_id = $1
		  GROUP BY a.account_id, a.code, a.name, a.type
		  ORDER BY a.code`,
		orgID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []GLAccountGroup
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var g GLAccountGroup
		if err := rows.Scan(&g.AccountID, &g.AccountCode, &g.AccountName, &g.AccountType,
			&g.Debit, &g.Credit, &g.Closing); err != nil {
			return nil, err
		}
		index[g.AccountID] = len(groups)
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	lines, err := r.journalFeed(ctx, orgID, from, to, nil)
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		i, ok := index[l.AccountID]
		if !ok {
			continue
		}
		groups[i].Lines = append(groups[i].Lines, GLLine{
			Date:        l.Date,
			Source:      l.Source,
			Reference:   l.Reference,
			Description: l.Description,
			Debit:       l.Debit,
			Credit:      l.Credit,
		})
	}
	// Opening balance = closing minus the period movement; the running balance
	// per line is then computed in a second pass.
	for i := range groups {
		groups[i].Opening = groups[i].Closing.Sub(groups[i].Debit).Add(groups[i].Credit)
		running := groups[i].Opening
		for j := range groups[i].Lines {
			running = running.Add(groups[i].Lines[j].Debit).Sub(groups[i].Lines[j].Credit)
			groups[i].Lines[j].Balance = running
		}
	}
	return groups, nil
}

type BankSummaryRow struct {
	AccountID      uuid.UUID       `json:"AccountID"`
	AccountCode    string          `json:"AccountCode"`
	AccountName    string          `json:"AccountName"`
	OpeningBalance decimal.Decimal `json:"OpeningBalance"`
	CashReceived   decimal.Decimal `json:"CashReceived"`
	CashSpent      decimal.Decimal `json:"CashSpent"`
	ClosingBalance decimal.Decimal `json:"ClosingBalance"`
}

// ExecutiveKPI is one line of the Executive Summary (Income, Gross Profit,
// Cash received, Accounts receivable, etc.). Values are absolute and already
// normalised to positive numbers for display.
type ExecutiveKPI struct {
	Title string          `json:"Title"`
	Value decimal.Decimal `json:"Value"`
}

// ExecutiveSummary derives high-level KPIs over the given period by
// re-using PnL + BankSummary + invoices.amount_due aggregates.
func (r *ReportRepository) ExecutiveSummary(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]ExecutiveKPI, error) {
	pnl, err := r.ProfitAndLoss(ctx, orgID, Period{From: from, To: to}, nil)
	if err != nil {
		return nil, err
	}
	bs, err := r.BalanceSheet(ctx, orgID, to, nil)
	if err != nil {
		return nil, err
	}
	bank, err := r.BankSummary(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
	var received, spent, closing decimal.Decimal
	for _, b := range bank {
		received = received.Add(b.CashReceived)
		spent = spent.Add(b.CashSpent)
		closing = closing.Add(b.ClosingBalance)
	}
	// Receivables / payables — approximate AR/AP via authorised invoices.
	var ar, ap decimal.Decimal
	// Both are as-at figures: invoice the contact after the report date and it
	// must not be in the receivable yet. This is exactly the row set (and the
	// total) the aged reports above return, so the KPI and the drill-down agree.
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_due),0) FROM invoices
		  WHERE organisation_id=$1 AND type=$2 AND status='AUTHORISED'
		    AND amount_due > 0 AND date <= $3::date`,
		orgID, models.InvoiceTypeAccRec, to).Scan(&ar); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_due),0) FROM invoices
		  WHERE organisation_id=$1 AND type=$2 AND status='AUTHORISED'
		    AND amount_due > 0 AND date <= $3::date`,
		orgID, models.InvoiceTypeAccPay, to).Scan(&ap); err != nil {
		return nil, err
	}

	return []ExecutiveKPI{
		{"Income", pnl.TotalIncome},
		{"Direct costs", pnl.TotalCostOfSales},
		{"Gross Profit", pnl.GrossProfit},
		{"Other Expenses", pnl.TotalExpenses},
		{"Net Profit", pnl.NetProfit},
		{"Cash received", received},
		{"Cash spent", spent},
		{"Cash surplus/(deficit)", received.Sub(spent)},
		{"Closing bank balance", closing},
		{"Accounts receivable", ar},
		{"Accounts payable", ap},
		{"Net assets", bs.TotalAssets.Sub(bs.TotalLiabilities)},
	}, nil
}

func (r *ReportRepository) BankSummary(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]BankSummaryRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, a.code, a.name,
		        COALESCE(SUM(CASE WHEN l.journal_date < $2 THEN l.net_amount END),0) AS opening,
		        COALESCE(SUM(CASE WHEN l.journal_date BETWEEN $2 AND $3 AND l.net_amount > 0 THEN l.net_amount END),0) AS received,
		        COALESCE(SUM(CASE WHEN l.journal_date BETWEEN $2 AND $3 AND l.net_amount < 0 THEN -l.net_amount END),0) AS spent,
		        COALESCE(SUM(CASE WHEN l.journal_date <= $3 THEN l.net_amount END),0) AS closing
		   FROM accounts a
		   LEFT JOIN (`+periodJoin("$3")+`
		   ) l ON l.account_id = a.account_id
		  WHERE a.organisation_id = $1
		    AND a.type = $4
		  GROUP BY a.account_id, a.code, a.name
		  ORDER BY a.code`,
		orgID, from, to, models.AccountTypeBank)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BankSummaryRow
	for rows.Next() {
		var row BankSummaryRow
		if err := rows.Scan(&row.AccountID, &row.AccountCode, &row.AccountName,
			&row.OpeningBalance, &row.CashReceived, &row.CashSpent, &row.ClosingBalance); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
