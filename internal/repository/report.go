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

// pnlAccountTypes are the accounts the Profit & Loss report aggregates.
var pnlAccountTypes = []string{
	models.AccountTypeRevenue, models.AccountTypeSales, models.AccountTypeDirectCosts,
	models.AccountTypeExpense, models.AccountTypeOverheads, models.AccountTypeDepreciatn,
	models.AccountTypeWages,
}

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

// TrialBalanceRow: one row per account with period movement (Debit/Credit) and
// year-to-date movement (YTDDebit/YTDCredit).
type TrialBalanceRow struct {
	AccountID   uuid.UUID       `json:"AccountID"`
	AccountCode string          `json:"AccountCode"`
	AccountName string          `json:"AccountName"`
	AccountType string          `json:"AccountType"`
	Debit       decimal.Decimal `json:"Debit"`
	Credit      decimal.Decimal `json:"Credit"`
	YTDDebit    decimal.Decimal `json:"YTDDebit"`
	YTDCredit   decimal.Decimal `json:"YTDCredit"`
}

// TrialBalance aggregates the period [from, to] plus year-to-date movement
// measured from the organisation's financial year start through `to`. Both
// windows are date-bounded inside the join — see the package comment.
func (r *ReportRepository) TrialBalance(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]TrialBalanceRow, error) {
	ytdFrom, err := r.FinancialYearStart(ctx, orgID, to)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, a.code, a.name, a.type,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $2 AND l.journal_date <= $3 AND l.net_amount > 0
		                          THEN  l.net_amount END),0) AS debit,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $2 AND l.journal_date <= $3 AND l.net_amount < 0
		                          THEN -l.net_amount END),0) AS credit,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $4 AND l.journal_date <= $3 AND l.net_amount > 0
		                          THEN  l.net_amount END),0) AS ytd_debit,
		        COALESCE(SUM(CASE WHEN l.journal_date >= $4 AND l.journal_date <= $3 AND l.net_amount < 0
		                          THEN -l.net_amount END),0) AS ytd_credit
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
		if err := rows.Scan(&row.AccountID, &row.AccountCode, &row.AccountName, &row.AccountType,
			&row.Debit, &row.Credit, &row.YTDDebit, &row.YTDCredit); err != nil {
			return nil, err
		}
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
		zero := decimal.Zero
		pnl.ComparativeIncome, pnl.ComparativeGross = &zero, &zero
		pnl.ComparativeNet, pnl.ComparativeCostSales, pnl.ComparativeExpenses = &zero, &zero, &zero
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
		zero := decimal.Zero
		bs.ComparativeTotalAssets, bs.ComparativeTotalLiabilities = &zero, &zero
		bs.ComparativeTotalEquity, bs.ComparativeRetainedEarnings = &zero, &zero
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

// AgedRow represents one contact in an aged receivables/payables report.
type AgedRow struct {
	ContactID   uuid.UUID       `json:"ContactID"`
	ContactName string          `json:"ContactName"`
	Current     decimal.Decimal `json:"Current"`
	Days1To30   decimal.Decimal `json:"1to30"`
	Days31To60  decimal.Decimal `json:"31to60"`
	Days61To90  decimal.Decimal `json:"61to90"`
	Days91Plus  decimal.Decimal `json:"91plus"`
	Total       decimal.Decimal `json:"Total"`
}

func (r *ReportRepository) Aged(ctx context.Context, orgID uuid.UUID, invoiceType string, asOf time.Time) ([]AgedRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT c.contact_id, c.name,
		        COALESCE(SUM(CASE WHEN $3::date - i.due_date <= 0 THEN i.amount_due END),0) AS current,
		        COALESCE(SUM(CASE WHEN $3::date - i.due_date BETWEEN 1 AND 30 THEN i.amount_due END),0) AS d1,
		        COALESCE(SUM(CASE WHEN $3::date - i.due_date BETWEEN 31 AND 60 THEN i.amount_due END),0) AS d2,
		        COALESCE(SUM(CASE WHEN $3::date - i.due_date BETWEEN 61 AND 90 THEN i.amount_due END),0) AS d3,
		        COALESCE(SUM(CASE WHEN $3::date - i.due_date > 90 THEN i.amount_due END),0) AS d4,
		        COALESCE(SUM(i.amount_due),0) AS total
		   FROM invoices i JOIN contacts c ON c.contact_id = i.contact_id
		  WHERE i.organisation_id=$1 AND i.type=$2
		    AND i.status='AUTHORISED' AND i.amount_due > 0
		    AND i.date <= $3::date
		  GROUP BY c.contact_id, c.name
		  ORDER BY c.name`,
		orgID, invoiceType, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgedRow
	for rows.Next() {
		var row AgedRow
		if err := rows.Scan(&row.ContactID, &row.ContactName,
			&row.Current, &row.Days1To30, &row.Days31To60, &row.Days61To90, &row.Days91Plus,
			&row.Total); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// SalesTaxRow powers the BAS / sales-tax report: net sales/purchases and the
// tax amounts charged / claimed per tax_type.
type SalesTaxRow struct {
	TaxType      string          `json:"TaxType"`
	NetSales     decimal.Decimal `json:"NetSales"`
	NetPurchases decimal.Decimal `json:"NetPurchases"`
	TaxCollected decimal.Decimal `json:"TaxCollected"`
	TaxPaid      decimal.Decimal `json:"TaxPaid"`
}

// SalesTaxByRate aggregates tax amounts per tax_type by inspecting
// invoice/credit-note line items directly (rather than the GL) so we can
// distinguish ACCREC from ACCPAY activity cleanly.
func (r *ReportRepository) SalesTaxByRate(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]SalesTaxRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(li.tax_type,''),'TAX') AS tax_type,
		       COALESCE(SUM(CASE WHEN i.type='ACCREC' THEN li.line_amount END),0) AS net_sales,
		       COALESCE(SUM(CASE WHEN i.type='ACCPAY' THEN li.line_amount END),0) AS net_purchases,
		       COALESCE(SUM(CASE WHEN i.type='ACCREC' THEN li.tax_amount  END),0) AS tax_collected,
		       COALESCE(SUM(CASE WHEN i.type='ACCPAY' THEN li.tax_amount  END),0) AS tax_paid
		  FROM invoices i
		  JOIN invoice_line_items li ON li.invoice_id = i.invoice_id
		 WHERE i.organisation_id=$1
		   AND i.status IN ('AUTHORISED','PAID')
		   AND i.date BETWEEN $2 AND $3
		 GROUP BY tax_type
		 ORDER BY tax_type`,
		orgID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SalesTaxRow
	for rows.Next() {
		var row SalesTaxRow
		if err := rows.Scan(&row.TaxType, &row.NetSales, &row.NetPurchases,
			&row.TaxCollected, &row.TaxPaid); err != nil {
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
// then date, which is what both drill-down reports need.
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
		 ORDER BY a.code, j.journal_date, j.journal_id`,
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
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_due),0) FROM invoices
		  WHERE organisation_id=$1 AND type=$2 AND status='AUTHORISED'`,
		orgID, models.InvoiceTypeAccRec).Scan(&ar); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_due),0) FROM invoices
		  WHERE organisation_id=$1 AND type=$2 AND status='AUTHORISED'`,
		orgID, models.InvoiceTypeAccPay).Scan(&ap); err != nil {
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
