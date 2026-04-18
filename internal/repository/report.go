package repository

// Reports derived from the GL. All five core Xero reports (Trial Balance,
// Profit & Loss, Balance Sheet, Aged Receivables/Payables, Bank Summary)
// are implemented by aggregating rows in `gl_journal_lines`, joined to
// `accounts` for the Chart of Accounts classification. Reports are returned
// as generic row structs so the handler can render them without extra work.
//
// References:
//   https://developer.xero.com/documentation/api/accounting/reports
//   https://central.xero.com/s/article/Run-the-Profit-and-Loss-report
//   https://central.xero.com/s/article/The-Trial-Balance-report

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

type ReportRepository struct {
	pool *pgxpool.Pool
}

// TrialBalanceRow: one row per account, with movement (YTD and period) plus
// the closing balance.
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

// TrialBalance: rows aggregated up to `asOf` (inclusive). The period debit
// and credit columns are equal to the YTD values in v1 since we don't yet
// track a financial year start — matches what Xero shows for first fiscal
// year orgs.
func (r *ReportRepository) TrialBalance(ctx context.Context, orgID uuid.UUID, asOf time.Time) ([]TrialBalanceRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, a.code, a.name, a.type,
		        COALESCE(SUM(CASE WHEN l.net_amount > 0 THEN l.net_amount END),0) AS debit,
		        COALESCE(SUM(CASE WHEN l.net_amount < 0 THEN -l.net_amount END),0) AS credit
		   FROM accounts a
		   LEFT JOIN gl_journal_lines l ON l.account_id = a.account_id
		   LEFT JOIN gl_journals       j ON j.journal_id = l.journal_id
		                                AND j.organisation_id = a.organisation_id
		                                AND j.journal_date <= $2
		  WHERE a.organisation_id = $1
		  GROUP BY a.account_id, a.code, a.name, a.type
		  ORDER BY a.code`,
		orgID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrialBalanceRow
	for rows.Next() {
		var row TrialBalanceRow
		if err := rows.Scan(&row.AccountID, &row.AccountCode, &row.AccountName, &row.AccountType,
			&row.Debit, &row.Credit); err != nil {
			return nil, err
		}
		row.YTDDebit = row.Debit
		row.YTDCredit = row.Credit
		out = append(out, row)
	}
	return out, rows.Err()
}

// PnLRow covers one line of the Profit & Loss statement.
type PnLRow struct {
	AccountID   uuid.UUID       `json:"AccountID"`
	AccountCode string          `json:"AccountCode"`
	AccountName string          `json:"AccountName"`
	AccountType string          `json:"AccountType"`
	Amount      decimal.Decimal `json:"Amount"`
}

type PnLReport struct {
	From             time.Time       `json:"FromDate"`
	To               time.Time       `json:"ToDate"`
	Income           []PnLRow        `json:"Income"`
	CostOfSales      []PnLRow        `json:"CostOfSales"`
	Expenses         []PnLRow        `json:"Expenses"`
	TotalIncome      decimal.Decimal `json:"TotalIncome"`
	GrossProfit      decimal.Decimal `json:"GrossProfit"`
	NetProfit        decimal.Decimal `json:"NetProfit"`
	TotalCostOfSales decimal.Decimal `json:"TotalCostOfSales"`
	TotalExpenses    decimal.Decimal `json:"TotalExpenses"`
}

func (r *ReportRepository) ProfitAndLoss(ctx context.Context, orgID uuid.UUID, from, to time.Time) (*PnLReport, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, a.code, a.name, a.type,
		        COALESCE(SUM(l.net_amount),0) AS balance
		   FROM accounts a
		   LEFT JOIN gl_journal_lines l ON l.account_id = a.account_id
		   LEFT JOIN gl_journals       j ON j.journal_id = l.journal_id
		                                AND j.organisation_id = a.organisation_id
		                                AND j.journal_date BETWEEN $2 AND $3
		  WHERE a.organisation_id = $1
		    AND a.type IN ('REVENUE','SALES','DIRECTCOSTS','EXPENSE','OVERHEADS',
		                   'DEPRECIATN','WAGESEXPENSE')
		  GROUP BY a.account_id, a.code, a.name, a.type
		  ORDER BY a.type, a.code`,
		orgID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pnl := &PnLReport{From: from, To: to}
	for rows.Next() {
		var row PnLRow
		if err := rows.Scan(&row.AccountID, &row.AccountCode, &row.AccountName, &row.AccountType, &row.Amount); err != nil {
			return nil, err
		}
		// Revenue accounts have credit-normal balances — our net_amount is
		// signed in debit-normal form, so revenue appears negative. Flip it
		// so the report shows positive numbers.
		switch row.AccountType {
		case models.AccountTypeRevenue, models.AccountTypeSales:
			row.Amount = row.Amount.Neg()
			pnl.Income = append(pnl.Income, row)
			pnl.TotalIncome = pnl.TotalIncome.Add(row.Amount)
		case models.AccountTypeDirectCosts:
			pnl.CostOfSales = append(pnl.CostOfSales, row)
			pnl.TotalCostOfSales = pnl.TotalCostOfSales.Add(row.Amount)
		default:
			pnl.Expenses = append(pnl.Expenses, row)
			pnl.TotalExpenses = pnl.TotalExpenses.Add(row.Amount)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pnl.GrossProfit = pnl.TotalIncome.Sub(pnl.TotalCostOfSales)
	pnl.NetProfit = pnl.GrossProfit.Sub(pnl.TotalExpenses)
	return pnl, nil
}

type BalanceSheetRow struct {
	AccountID   uuid.UUID       `json:"AccountID"`
	AccountCode string          `json:"AccountCode"`
	AccountName string          `json:"AccountName"`
	AccountType string          `json:"AccountType"`
	Amount      decimal.Decimal `json:"Amount"`
}

type BalanceSheet struct {
	AsOf             time.Time         `json:"AsOf"`
	Assets           []BalanceSheetRow `json:"Assets"`
	Liabilities      []BalanceSheetRow `json:"Liabilities"`
	Equity           []BalanceSheetRow `json:"Equity"`
	TotalAssets      decimal.Decimal   `json:"TotalAssets"`
	TotalLiabilities decimal.Decimal   `json:"TotalLiabilities"`
	TotalEquity      decimal.Decimal   `json:"TotalEquity"`
	RetainedEarnings decimal.Decimal   `json:"RetainedEarnings"`
}

func (r *ReportRepository) BalanceSheet(ctx context.Context, orgID uuid.UUID, asOf time.Time) (*BalanceSheet, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, a.code, a.name, a.type,
		        COALESCE(SUM(l.net_amount),0) AS balance
		   FROM accounts a
		   LEFT JOIN gl_journal_lines l ON l.account_id = a.account_id
		   LEFT JOIN gl_journals       j ON j.journal_id = l.journal_id
		                                AND j.organisation_id = a.organisation_id
		                                AND j.journal_date <= $2
		  WHERE a.organisation_id = $1
		  GROUP BY a.account_id, a.code, a.name, a.type
		  ORDER BY a.type, a.code`,
		orgID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bs := &BalanceSheet{AsOf: asOf}
	for rows.Next() {
		var row BalanceSheetRow
		if err := rows.Scan(&row.AccountID, &row.AccountCode, &row.AccountName, &row.AccountType, &row.Amount); err != nil {
			return nil, err
		}
		switch row.AccountType {
		case models.AccountTypeBank, models.AccountTypeCurrent, models.AccountTypeFixed,
			models.AccountTypePrepayment, models.AccountTypeInventory, models.AccountTypeNonCurrent:
			bs.Assets = append(bs.Assets, row)
			bs.TotalAssets = bs.TotalAssets.Add(row.Amount)
		case models.AccountTypeCurrLiab, models.AccountTypeLiability,
			models.AccountTypeTermLiab, models.AccountTypePAYGLiab, models.AccountTypeSuperLiab:
			row.Amount = row.Amount.Neg()
			bs.Liabilities = append(bs.Liabilities, row)
			bs.TotalLiabilities = bs.TotalLiabilities.Add(row.Amount)
		case models.AccountTypeEquity:
			row.Amount = row.Amount.Neg()
			bs.Equity = append(bs.Equity, row)
			bs.TotalEquity = bs.TotalEquity.Add(row.Amount)
		case models.AccountTypeRevenue, models.AccountTypeSales,
			models.AccountTypeDirectCosts, models.AccountTypeExpense,
			models.AccountTypeOverheads, models.AccountTypeDepreciatn, models.AccountTypeWages:
			// Net P/L rolls into retained earnings.
			bs.RetainedEarnings = bs.RetainedEarnings.Sub(row.Amount)
		}
	}
	bs.TotalEquity = bs.TotalEquity.Add(bs.RetainedEarnings)
	return bs, rows.Err()
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

// JournalFeedRow is one row per GL posting for the Journal report.
type JournalFeedRow struct {
	Date        time.Time       `json:"Date"`
	Source      string          `json:"Source"`
	Reference   string          `json:"Reference"`
	AccountID   uuid.UUID       `json:"AccountID"`
	AccountCode string          `json:"AccountCode"`
	AccountName string          `json:"AccountName"`
	Debit       decimal.Decimal `json:"Debit"`
	Credit      decimal.Decimal `json:"Credit"`
}

// JournalFeed returns every GL line between `from` and `to` for the Journal
// report handler.
func (r *ReportRepository) JournalFeed(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]JournalFeedRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT j.journal_date, j.source_type, COALESCE(j.reference,''),
		       a.account_id, a.code, a.name,
		       CASE WHEN l.net_amount > 0 THEN  l.net_amount ELSE 0 END AS debit,
		       CASE WHEN l.net_amount < 0 THEN -l.net_amount ELSE 0 END AS credit
		  FROM gl_journals j
		  JOIN gl_journal_lines l ON l.journal_id = j.journal_id
		  JOIN accounts a         ON a.account_id = l.account_id
		 WHERE j.organisation_id=$1 AND j.journal_date BETWEEN $2 AND $3
		 ORDER BY j.journal_date, j.journal_id, a.code`,
		orgID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalFeedRow
	for rows.Next() {
		var row JournalFeedRow
		if err := rows.Scan(&row.Date, &row.Source, &row.Reference,
			&row.AccountID, &row.AccountCode, &row.AccountName,
			&row.Debit, &row.Credit); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
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
	pnl, err := r.ProfitAndLoss(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
	bs, err := r.BalanceSheet(ctx, orgID, to)
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
	_ = r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_due),0) FROM invoices
		  WHERE organisation_id=$1 AND type=$2 AND status='AUTHORISED'`,
		orgID, models.InvoiceTypeAccRec).Scan(&ar)
	_ = r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_due),0) FROM invoices
		  WHERE organisation_id=$1 AND type=$2 AND status='AUTHORISED'`,
		orgID, models.InvoiceTypeAccPay).Scan(&ap)

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
		        COALESCE(SUM(CASE WHEN j.journal_date < $2 THEN l.net_amount END),0) AS opening,
		        COALESCE(SUM(CASE WHEN j.journal_date BETWEEN $2 AND $3 AND l.net_amount > 0 THEN l.net_amount END),0) AS received,
		        COALESCE(SUM(CASE WHEN j.journal_date BETWEEN $2 AND $3 AND l.net_amount < 0 THEN -l.net_amount END),0) AS spent,
		        COALESCE(SUM(CASE WHEN j.journal_date <= $3 THEN l.net_amount END),0) AS closing
		   FROM accounts a
		   LEFT JOIN gl_journal_lines l ON l.account_id = a.account_id
		   LEFT JOIN gl_journals       j ON j.journal_id = l.journal_id
		                                AND j.organisation_id = a.organisation_id
		  WHERE a.organisation_id = $1
		    AND a.type = 'BANK'
		  GROUP BY a.account_id, a.code, a.name
		  ORDER BY a.code`,
		orgID, from, to)
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
