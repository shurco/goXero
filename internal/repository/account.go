package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

type AccountRepository struct {
	pool *pgxpool.Pool
}

const accountColumns = `
	account_id, code, name, type,
	COALESCE(bank_account_number,''), COALESCE(bank_account_type,''), COALESCE(currency_code,''),
	status, COALESCE(description,''), COALESCE(tax_type,''),
	enable_payments_to_account, show_in_expense_claims,
	COALESCE(system_account,''),
	COALESCE(reporting_code,''), COALESCE(reporting_code_name,''),
	has_attachments, auto_reconcile, updated_date_utc`

func scanAccount(row pgx.Row) (*models.Account, error) {
	a := &models.Account{}
	err := row.Scan(
		&a.AccountID, &a.Code, &a.Name, &a.Type,
		&a.BankAccountNumber, &a.BankAccountType, &a.CurrencyCode,
		&a.Status, &a.Description, &a.TaxType,
		&a.EnablePaymentsToAccount, &a.ShowInExpenseClaims,
		&a.SystemAccount,
		&a.ReportingCode, &a.ReportingCodeName,
		&a.HasAttachments, &a.AutoReconcile, &a.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	deriveClass(a)
	return a, nil
}

// deriveClass sets the reporting class Xero gives an account of this type.
//
// Class is not an independent fact about an account — Xero computes it from
// Type — so it is derived at every boundary rather than taken from the client,
// which may omit it or send one that disagrees with the type. A chart using
// types this build has never seen gets no class, which is what its type maps to.
func deriveClass(a *models.Account) {
	a.Class = models.AccountClassForType(a.Type)
}

type AccountFilter struct {
	Status string
	Type   string
	Search string
}

func (r *AccountRepository) List(ctx context.Context, orgID uuid.UUID, f AccountFilter) ([]models.Account, error) {
	var sb strings.Builder
	sb.WriteString("SELECT " + accountColumns + " FROM accounts WHERE organisation_id=$1")
	args := []any{orgID}

	if f.Status != "" {
		args = append(args, f.Status)
		sb.WriteString(" AND status=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Type != "" {
		args = append(args, f.Type)
		sb.WriteString(" AND type=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		sb.WriteString(" AND (name ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(" OR code ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(")")
	}
	sb.WriteString(" ORDER BY code")

	rows, err := r.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *a)
	}
	return list, rows.Err()
}

func (r *AccountRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Account, error) {
	q := "SELECT " + accountColumns + " FROM accounts WHERE organisation_id=$1 AND account_id=$2"
	return scanAccount(r.pool.QueryRow(ctx, q, orgID, id))
}

// StandardChart returns the standard chart of accounts — the 58 accounts of the
// captured Xero organisation, held as reference data by migration 00029 from
// migrations/data/xero/accounts.csv — shaped as accounts a caller can create, so
// that "Import standard chart" posts what it reads rather than a chart written
// into the frontend.
//
// Two fields are resolved here rather than stored, and neither is invented:
//
//   - TaxType. The reference table carries the tax rate by the name Xero prints
//     ("Tax on Purchases (8.25%)"), and the type the API speaks ('INPUT') is a
//     row of *this organisation's* tax_rates, so it is looked up per tenant. An
//     organisation with no rate by that name gets an account with no tax type,
//     not one borrowed from another chart.
//
// Class is derived here the same way every other path derives it (deriveClass),
// rather than stored a second time where it could drift.
//
// No balance is returned, because the reference table carries none: an opening
// figure belongs to the books it was read from.
func (r *AccountRepository) StandardChart(ctx context.Context, orgID uuid.UUID) ([]models.Account, error) {
	const q = `
		SELECT s.code, s.name, s.type, COALESCE(t.tax_type, ''), s.description
		  FROM standard_chart_accounts s
		  LEFT JOIN tax_rates t ON t.organisation_id = $1 AND t.name = s.tax_rate_name
		 ORDER BY s.display_order`
	rows, err := r.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Account, 0, 58)
	for rows.Next() {
		var a models.Account
		if err := rows.Scan(&a.Code, &a.Name, &a.Type, &a.TaxType, &a.Description); err != nil {
			return nil, err
		}
		a.Status = "ACTIVE"
		deriveClass(&a)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AccountRepository) Create(ctx context.Context, orgID uuid.UUID, a *models.Account) error {
	deriveClass(a)
	// auto_reconcile is written here as well as in Update: the create handler
	// echoes the payload back, so dropping the field would answer with a value
	// the row does not hold.
	q := `INSERT INTO accounts (
		organisation_id, code, name, type, bank_account_number, bank_account_type,
		currency_code, status, description, tax_type,
		enable_payments_to_account, show_in_expense_claims, class, reporting_code, reporting_code_name,
		auto_reconcile
	) VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE(NULLIF($8,''),'ACTIVE'),$9,$10,$11,$12,$13,$14,$15,$16)
	RETURNING account_id, updated_date_utc`
	return r.pool.QueryRow(ctx, q,
		orgID, a.Code, a.Name, a.Type, a.BankAccountNumber, a.BankAccountType,
		a.CurrencyCode, a.Status, a.Description, a.TaxType,
		a.EnablePaymentsToAccount, a.ShowInExpenseClaims, a.Class, a.ReportingCode, a.ReportingCodeName,
		a.AutoReconcile,
	).Scan(&a.AccountID, &a.UpdatedDateUTC)
}

func (r *AccountRepository) Update(ctx context.Context, orgID uuid.UUID, a *models.Account) error {
	deriveClass(a)
	q := `UPDATE accounts SET
		code=$3, name=$4, type=$5,
		bank_account_number=NULLIF($6,''), bank_account_type=NULLIF($7,''), currency_code=NULLIF($8,''),
		status=$9, description=$10, tax_type=NULLIF($11,''),
		enable_payments_to_account=$12, show_in_expense_claims=$13,
		class=NULLIF($14,''), reporting_code=NULLIF($15,''), reporting_code_name=NULLIF($16,''),
		auto_reconcile=$17,
		updated_date_utc = now()
		WHERE organisation_id=$1 AND account_id=$2
		RETURNING updated_date_utc`
	return r.pool.QueryRow(ctx, q,
		orgID, a.AccountID, a.Code, a.Name, a.Type,
		a.BankAccountNumber, a.BankAccountType, a.CurrencyCode,
		a.Status, a.Description, a.TaxType,
		a.EnablePaymentsToAccount, a.ShowInExpenseClaims,
		a.Class, a.ReportingCode, a.ReportingCodeName, a.AutoReconcile,
	).Scan(&a.UpdatedDateUTC)
}

func (r *AccountRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE accounts SET status='ARCHIVED', updated_date_utc=now()
		 WHERE organisation_id=$1 AND account_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TaxRateRepository -----------------------------------------------------

type TaxRateRepository struct {
	pool *pgxpool.Pool
}

func (r *TaxRateRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.TaxRate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT tax_rate_id, name, tax_type, COALESCE(report_tax_type,''),
			can_apply_to_assets, can_apply_to_equity, can_apply_to_expenses,
			can_apply_to_liabilities, can_apply_to_revenue,
			display_tax_rate, effective_rate, status
		 FROM tax_rates WHERE organisation_id=$1 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.TaxRate
	for rows.Next() {
		var t models.TaxRate
		if err := rows.Scan(
			&t.TaxRateID, &t.Name, &t.TaxType, &t.ReportTaxType,
			&t.CanApplyToAssets, &t.CanApplyToEquity, &t.CanApplyToExpenses,
			&t.CanApplyToLiabilities, &t.CanApplyToRevenue,
			&t.DisplayTaxRate, &t.EffectiveRate, &t.Status,
		); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

func (r *TaxRateRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.TaxRate, error) {
	var t models.TaxRate
	err := r.pool.QueryRow(ctx,
		`SELECT tax_rate_id, name, tax_type, COALESCE(report_tax_type,''),
			can_apply_to_assets, can_apply_to_equity, can_apply_to_expenses,
			can_apply_to_liabilities, can_apply_to_revenue,
			display_tax_rate, effective_rate, status
		 FROM tax_rates WHERE organisation_id=$1 AND tax_rate_id=$2`, orgID, id).Scan(
		&t.TaxRateID, &t.Name, &t.TaxType, &t.ReportTaxType,
		&t.CanApplyToAssets, &t.CanApplyToEquity, &t.CanApplyToExpenses,
		&t.CanApplyToLiabilities, &t.CanApplyToRevenue,
		&t.DisplayTaxRate, &t.EffectiveRate, &t.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *TaxRateRepository) Create(ctx context.Context, orgID uuid.UUID, t *models.TaxRate) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO tax_rates (organisation_id, name, tax_type, report_tax_type, display_tax_rate, effective_rate)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING tax_rate_id`,
		orgID, t.Name, t.TaxType, t.ReportTaxType, t.DisplayTaxRate, t.EffectiveRate,
	).Scan(&t.TaxRateID)
}

func (r *TaxRateRepository) Update(ctx context.Context, orgID uuid.UUID, t *models.TaxRate) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE tax_rates SET
			name=$3, tax_type=$4, report_tax_type=NULLIF($5,''),
			display_tax_rate=$6, effective_rate=$7,
			status=COALESCE(NULLIF($8,''), status),
			updated_at = now()
		 WHERE organisation_id=$1 AND tax_rate_id=$2`,
		orgID, t.TaxRateID, t.Name, t.TaxType, t.ReportTaxType,
		t.DisplayTaxRate, t.EffectiveRate, t.Status)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete archives the tax rate (Xero never physically deletes tax rates).
func (r *TaxRateRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE tax_rates SET status='DELETED', updated_at=now()
		 WHERE organisation_id=$1 AND tax_rate_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAutoReconcile flips the per-account setting behind Xero's "Turn
// auto-reconcile on". It is deliberately its own statement rather than a pass
// through Update: the toggle in the reconcile banner must not be able to
// rewrite the account's name as a side effect.
func (r *AccountRepository) SetAutoReconcile(ctx context.Context, orgID, accountID uuid.UUID, enabled bool) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE accounts SET auto_reconcile=$3, updated_date_utc=now()
		 WHERE organisation_id=$1 AND account_id=$2`, orgID, accountID, enabled)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
