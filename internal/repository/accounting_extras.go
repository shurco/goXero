package repository

// Repositories for the Xero accounting resources introduced in migration
// 00013 (prepayments, overpayments, repeating invoices, batch payments,
// linked transactions, employees, receipts, expense claims).
//
// These resources share a lot of shape with the ones implemented earlier
// (Invoice / CreditNote / Payment), so they reuse the same GL posting hooks
// in gl.go when they impact the ledger.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

// orgBaseCurrency returns the organisation's configured base currency, falling
// back to USD when unset. Used by extras repos that need to stamp a currency
// code on inserts when the caller didn't specify one (matching Xero behaviour).
func orgBaseCurrency(ctx context.Context, q pgxQueryer, orgID uuid.UUID) string {
	var code string
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(NULLIF(base_currency,''),'USD') FROM organisations WHERE organisation_id=$1`,
		orgID).Scan(&code); err != nil || code == "" {
		return "USD"
	}
	return code
}

// pgxQueryer abstracts *pgxpool.Pool and pgx.Tx to share QueryRow helpers.
type pgxQueryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// -----------------------------------------------------------------------------
// Prepayments
// -----------------------------------------------------------------------------

type PrepaymentRepository struct{ pool *pgxpool.Pool }

func (r *PrepaymentRepository) Create(ctx context.Context, orgID uuid.UUID, p *models.Prepayment) error {
	if p.PrepaymentID == uuid.Nil {
		p.PrepaymentID = uuid.New()
	}
	if p.Status == "" {
		p.Status = "AUTHORISED"
	}
	if p.RemainingCredit.IsZero() {
		p.RemainingCredit = p.Total
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if p.CurrencyCode == "" {
		p.CurrencyCode = orgBaseCurrency(ctx, tx, orgID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO prepayments
		    (prepayment_id, organisation_id, contact_id, type, status,
		     currency_code, date, reference,
		     sub_total, total_tax, total, remaining_credit)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11,$12)`,
		p.PrepaymentID, orgID, p.ContactID, p.Type, p.Status,
		p.CurrencyCode, p.Date, p.Reference,
		p.SubTotal, p.TotalTax, p.Total, p.RemainingCredit); err != nil {
		return err
	}
	if p.BankAccountID != nil {
		if err := postPrepaymentJournal(ctx, tx, orgID, *p.BankAccountID, p); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *PrepaymentRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.Prepayment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT prepayment_id, contact_id, type, status, currency_code,
		       date, COALESCE(reference,''), sub_total, total_tax, total, remaining_credit, updated_date_utc
		  FROM prepayments WHERE organisation_id=$1 AND status <> 'DELETED' ORDER BY date DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Prepayment
	for rows.Next() {
		var p models.Prepayment
		var d *time.Time
		if err := rows.Scan(&p.PrepaymentID, &p.ContactID, &p.Type, &p.Status, &p.CurrencyCode,
			&d, &p.Reference, &p.SubTotal, &p.TotalTax, &p.Total, &p.RemainingCredit, &p.UpdatedDateUTC); err != nil {
			return nil, err
		}
		p.Date = d
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PrepaymentRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Prepayment, error) {
	var p models.Prepayment
	var d *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT prepayment_id, contact_id, type, status, currency_code,
		       date, COALESCE(reference,''), sub_total, total_tax, total, remaining_credit, updated_date_utc
		  FROM prepayments WHERE organisation_id=$1 AND prepayment_id=$2`, orgID, id,
	).Scan(&p.PrepaymentID, &p.ContactID, &p.Type, &p.Status, &p.CurrencyCode,
		&d, &p.Reference, &p.SubTotal, &p.TotalTax, &p.Total, &p.RemainingCredit, &p.UpdatedDateUTC)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.Date = d
	return &p, nil
}

func (r *PrepaymentRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE prepayments SET status='DELETED', updated_date_utc=now()
		  WHERE organisation_id=$1 AND prepayment_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// Overpayments
// -----------------------------------------------------------------------------

type OverpaymentRepository struct{ pool *pgxpool.Pool }

func (r *OverpaymentRepository) Create(ctx context.Context, orgID uuid.UUID, o *models.Overpayment) error {
	if o.OverpaymentID == uuid.Nil {
		o.OverpaymentID = uuid.New()
	}
	if o.Status == "" {
		o.Status = "AUTHORISED"
	}
	if o.RemainingCredit.IsZero() {
		o.RemainingCredit = o.Total
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if o.CurrencyCode == "" {
		o.CurrencyCode = orgBaseCurrency(ctx, tx, orgID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO overpayments (overpayment_id, organisation_id, contact_id, type, status,
		    currency_code, date, reference, total, remaining_credit)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10)`,
		o.OverpaymentID, orgID, o.ContactID, o.Type, o.Status,
		o.CurrencyCode, o.Date, o.Reference, o.Total, o.RemainingCredit); err != nil {
		return err
	}
	if o.BankAccountID != nil {
		if err := postOverpaymentJournal(ctx, tx, orgID, *o.BankAccountID, o); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *OverpaymentRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.Overpayment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT overpayment_id, contact_id, type, status, currency_code,
		       date, COALESCE(reference,''), total, remaining_credit, updated_date_utc
		  FROM overpayments WHERE organisation_id=$1 AND status <> 'DELETED' ORDER BY date DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Overpayment
	for rows.Next() {
		var o models.Overpayment
		var d *time.Time
		if err := rows.Scan(&o.OverpaymentID, &o.ContactID, &o.Type, &o.Status, &o.CurrencyCode,
			&d, &o.Reference, &o.Total, &o.RemainingCredit, &o.UpdatedDateUTC); err != nil {
			return nil, err
		}
		o.Date = d
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *OverpaymentRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Overpayment, error) {
	var o models.Overpayment
	var d *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT overpayment_id, contact_id, type, status, currency_code,
		       date, COALESCE(reference,''), total, remaining_credit, updated_date_utc
		  FROM overpayments WHERE organisation_id=$1 AND overpayment_id=$2`, orgID, id,
	).Scan(&o.OverpaymentID, &o.ContactID, &o.Type, &o.Status, &o.CurrencyCode,
		&d, &o.Reference, &o.Total, &o.RemainingCredit, &o.UpdatedDateUTC)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	o.Date = d
	return &o, nil
}

func (r *OverpaymentRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE overpayments SET status='DELETED', updated_date_utc=now()
		  WHERE organisation_id=$1 AND overpayment_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// Repeating invoices
// -----------------------------------------------------------------------------

type RepeatingInvoiceRepository struct{ pool *pgxpool.Pool }

func (r *RepeatingInvoiceRepository) Create(ctx context.Context, orgID uuid.UUID, ri *models.RepeatingInvoice) error {
	if ri.RepeatingInvoiceID == uuid.Nil {
		ri.RepeatingInvoiceID = uuid.New()
	}
	if ri.Status == "" {
		ri.Status = "DRAFT"
	}
	if ri.LineAmountTypes == "" {
		ri.LineAmountTypes = "Exclusive"
	}
	if ri.Schedule.Unit == "" {
		ri.Schedule.Unit = "MONTHLY"
	}
	if ri.Schedule.Period == 0 {
		ri.Schedule.Period = 1
	}
	if ri.Schedule.DueDateType == "" {
		ri.Schedule.DueDateType = "DAYSAFTERBILLDATE"
	}
	// Compute totals if not supplied.
	if ri.Total.IsZero() {
		for _, li := range ri.LineItems {
			ri.SubTotal = ri.SubTotal.Add(li.LineAmount)
			ri.TotalTax = ri.TotalTax.Add(li.TaxAmount)
		}
		ri.Total = ri.SubTotal.Add(ri.TotalTax)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if ri.CurrencyCode == "" {
		ri.CurrencyCode = orgBaseCurrency(ctx, tx, orgID)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO repeating_invoices
		    (repeating_invoice_id, organisation_id, contact_id, type, status, reference,
		     line_amount_types, currency_code, branding_theme_id,
		     period, unit, due_date, due_date_type,
		     start_date, next_scheduled_date, end_date,
		     sub_total, total_tax, total)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		ri.RepeatingInvoiceID, orgID, ri.ContactID, ri.Type, ri.Status, ri.Reference,
		ri.LineAmountTypes, ri.CurrencyCode, ri.BrandingThemeID,
		ri.Schedule.Period, ri.Schedule.Unit, ri.Schedule.DueDate, ri.Schedule.DueDateType,
		ri.Schedule.StartDate, ri.Schedule.NextScheduledDate, ri.Schedule.EndDate,
		ri.SubTotal, ri.TotalTax, ri.Total,
	); err != nil {
		return err
	}
	for _, li := range ri.LineItems {
		if _, err := tx.Exec(ctx, `
			INSERT INTO repeating_invoice_line_items
			    (repeating_invoice_id, description, quantity, unit_amount,
			     account_code, tax_type, tax_amount, line_amount, item_code, discount_rate)
			VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,NULLIF($9,''),$10)`,
			ri.RepeatingInvoiceID, li.Description, li.Quantity, li.UnitAmount,
			li.AccountCode, li.TaxType, li.TaxAmount, li.LineAmount, li.ItemCode, li.DiscountRate,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *RepeatingInvoiceRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.RepeatingInvoice, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT repeating_invoice_id, contact_id, type, status, COALESCE(reference,''),
		       line_amount_types, currency_code, branding_theme_id,
		       period, unit, due_date, due_date_type,
		       start_date, next_scheduled_date, end_date,
		       sub_total, total_tax, total, updated_date_utc
		  FROM repeating_invoices WHERE organisation_id=$1 AND status<>'DELETED'
		  ORDER BY updated_date_utc DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RepeatingInvoice
	for rows.Next() {
		var ri models.RepeatingInvoice
		if err := rows.Scan(&ri.RepeatingInvoiceID, &ri.ContactID, &ri.Type, &ri.Status, &ri.Reference,
			&ri.LineAmountTypes, &ri.CurrencyCode, &ri.BrandingThemeID,
			&ri.Schedule.Period, &ri.Schedule.Unit, &ri.Schedule.DueDate, &ri.Schedule.DueDateType,
			&ri.Schedule.StartDate, &ri.Schedule.NextScheduledDate, &ri.Schedule.EndDate,
			&ri.SubTotal, &ri.TotalTax, &ri.Total, &ri.UpdatedDateUTC); err != nil {
			return nil, err
		}
		out = append(out, ri)
	}
	return out, rows.Err()
}

func (r *RepeatingInvoiceRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.RepeatingInvoice, error) {
	var ri models.RepeatingInvoice
	err := r.pool.QueryRow(ctx, `
		SELECT repeating_invoice_id, contact_id, type, status, COALESCE(reference,''),
		       line_amount_types, currency_code, branding_theme_id,
		       period, unit, due_date, due_date_type,
		       start_date, next_scheduled_date, end_date,
		       sub_total, total_tax, total, updated_date_utc
		  FROM repeating_invoices WHERE organisation_id=$1 AND repeating_invoice_id=$2`, orgID, id,
	).Scan(&ri.RepeatingInvoiceID, &ri.ContactID, &ri.Type, &ri.Status, &ri.Reference,
		&ri.LineAmountTypes, &ri.CurrencyCode, &ri.BrandingThemeID,
		&ri.Schedule.Period, &ri.Schedule.Unit, &ri.Schedule.DueDate, &ri.Schedule.DueDateType,
		&ri.Schedule.StartDate, &ri.Schedule.NextScheduledDate, &ri.Schedule.EndDate,
		&ri.SubTotal, &ri.TotalTax, &ri.Total, &ri.UpdatedDateUTC)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT line_item_id, COALESCE(description,''), quantity, unit_amount,
		       COALESCE(account_code,''), COALESCE(tax_type,''),
		       tax_amount, line_amount, COALESCE(item_code,''), COALESCE(discount_rate,0)
		  FROM repeating_invoice_line_items WHERE repeating_invoice_id=$1 ORDER BY line_item_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var li models.LineItem
		if err := rows.Scan(&li.LineItemID, &li.Description, &li.Quantity, &li.UnitAmount,
			&li.AccountCode, &li.TaxType, &li.TaxAmount, &li.LineAmount, &li.ItemCode, &li.DiscountRate); err != nil {
			return nil, err
		}
		ri.LineItems = append(ri.LineItems, li)
	}
	return &ri, rows.Err()
}

func (r *RepeatingInvoiceRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE repeating_invoices SET status='DELETED', updated_date_utc=now()
		  WHERE organisation_id=$1 AND repeating_invoice_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// Batch payments
// -----------------------------------------------------------------------------

type BatchPaymentRepository struct{ pool *pgxpool.Pool }

// Create inserts the batch header plus each child payment row (which in turn
// gets posted to the GL by PaymentRepository-style logic, without re-using the
// same repo to avoid fee duplication).
func (r *BatchPaymentRepository) Create(ctx context.Context, orgID uuid.UUID, bp *models.BatchPayment) error {
	if bp.BatchPaymentID == uuid.Nil {
		bp.BatchPaymentID = uuid.New()
	}
	if bp.Status == "" {
		bp.Status = "AUTHORISED"
	}
	var total decimal.Decimal
	for _, p := range bp.Payments {
		total = total.Add(p.Amount)
	}
	bp.TotalAmount = total

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO batch_payments
		    (batch_payment_id, organisation_id, account_id, date, reference, narrative, details, status, total_amount)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9)`,
		bp.BatchPaymentID, orgID, bp.AccountID, bp.Date, bp.Reference, bp.Narrative, bp.Details, bp.Status, bp.TotalAmount,
	); err != nil {
		return err
	}
	for i := range bp.Payments {
		p := &bp.Payments[i]
		if p.PaymentID == uuid.Nil {
			p.PaymentID = uuid.New()
		}
		p.AccountID = &bp.AccountID
		if p.Date.IsZero() && bp.Date != nil {
			p.Date = *bp.Date
		}
		if p.Status == "" {
			p.Status = "AUTHORISED"
		}
		if p.PaymentType == "" {
			p.PaymentType = "ACCRECPAYMENT"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO payments
			    (payment_id, organisation_id, invoice_id, account_id, batch_payment_id,
			     date, amount, reference, payment_type, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10)`,
			p.PaymentID, orgID, p.InvoiceID, p.AccountID, bp.BatchPaymentID,
			p.Date, p.Amount, p.Reference, p.PaymentType, p.Status,
		); err != nil {
			return err
		}
		if err := postPaymentJournal(ctx, tx, orgID, p); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *BatchPaymentRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.BatchPayment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT batch_payment_id, account_id, date, COALESCE(reference,''),
		       COALESCE(narrative,''), COALESCE(details,''), status, total_amount, updated_date_utc
		  FROM batch_payments WHERE organisation_id=$1 AND status<>'DELETED' ORDER BY date DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BatchPayment
	for rows.Next() {
		var b models.BatchPayment
		var d *time.Time
		if err := rows.Scan(&b.BatchPaymentID, &b.AccountID, &d, &b.Reference,
			&b.Narrative, &b.Details, &b.Status, &b.TotalAmount, &b.UpdatedDateUTC); err != nil {
			return nil, err
		}
		b.Date = d
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *BatchPaymentRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.BatchPayment, error) {
	var b models.BatchPayment
	var d *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT batch_payment_id, account_id, date, COALESCE(reference,''),
		       COALESCE(narrative,''), COALESCE(details,''), status, total_amount, updated_date_utc
		  FROM batch_payments WHERE organisation_id=$1 AND batch_payment_id=$2`, orgID, id,
	).Scan(&b.BatchPaymentID, &b.AccountID, &d, &b.Reference,
		&b.Narrative, &b.Details, &b.Status, &b.TotalAmount, &b.UpdatedDateUTC)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	b.Date = d
	// Fetch child payments.
	rows, err := r.pool.Query(ctx, `
		SELECT payment_id, invoice_id, account_id, date, amount, COALESCE(reference,''),
		       payment_type, status
		  FROM payments WHERE organisation_id=$1 AND batch_payment_id=$2`, orgID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p models.Payment
		if err := rows.Scan(&p.PaymentID, &p.InvoiceID, &p.AccountID, &p.Date, &p.Amount,
			&p.Reference, &p.PaymentType, &p.Status); err != nil {
			return nil, err
		}
		b.Payments = append(b.Payments, p)
	}
	return &b, rows.Err()
}

// -----------------------------------------------------------------------------
// Linked transactions
// -----------------------------------------------------------------------------

type LinkedTransactionRepository struct{ pool *pgxpool.Pool }

func (r *LinkedTransactionRepository) Create(ctx context.Context, orgID uuid.UUID, l *models.LinkedTransaction) error {
	if l.LinkedTransactionID == uuid.Nil {
		l.LinkedTransactionID = uuid.New()
	}
	if l.Status == "" {
		l.Status = "DRAFT"
	}
	if l.Type == "" {
		l.Type = "BILLABLE_EXPENSE"
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO linked_transactions
		    (linked_transaction_id, organisation_id, source_transaction_id, source_line_item_id,
		     target_transaction_id, target_line_item_id, contact_id, type, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		l.LinkedTransactionID, orgID, l.SourceTransactionID, l.SourceLineItemID,
		l.TargetTransactionID, l.TargetLineItemID, l.ContactID, l.Type, l.Status)
	return err
}

func (r *LinkedTransactionRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.LinkedTransaction, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT linked_transaction_id, source_transaction_id, source_line_item_id,
		       target_transaction_id, target_line_item_id, contact_id, type, status, updated_date_utc
		  FROM linked_transactions WHERE organisation_id=$1 ORDER BY updated_date_utc DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.LinkedTransaction
	for rows.Next() {
		var l models.LinkedTransaction
		if err := rows.Scan(&l.LinkedTransactionID, &l.SourceTransactionID, &l.SourceLineItemID,
			&l.TargetTransactionID, &l.TargetLineItemID, &l.ContactID, &l.Type, &l.Status, &l.UpdatedDateUTC); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *LinkedTransactionRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.LinkedTransaction, error) {
	var l models.LinkedTransaction
	err := r.pool.QueryRow(ctx, `
		SELECT linked_transaction_id, source_transaction_id, source_line_item_id,
		       target_transaction_id, target_line_item_id, contact_id, type, status, updated_date_utc
		  FROM linked_transactions WHERE organisation_id=$1 AND linked_transaction_id=$2`, orgID, id,
	).Scan(&l.LinkedTransactionID, &l.SourceTransactionID, &l.SourceLineItemID,
		&l.TargetTransactionID, &l.TargetLineItemID, &l.ContactID, &l.Type, &l.Status, &l.UpdatedDateUTC)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &l, nil
}

func (r *LinkedTransactionRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM linked_transactions WHERE organisation_id=$1 AND linked_transaction_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// Employees
// -----------------------------------------------------------------------------

type EmployeeRepository struct{ pool *pgxpool.Pool }

func (r *EmployeeRepository) Create(ctx context.Context, orgID uuid.UUID, e *models.Employee) error {
	if e.EmployeeID == uuid.Nil {
		e.EmployeeID = uuid.New()
	}
	if e.Status == "" {
		e.Status = "ACTIVE"
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO employees (employee_id, organisation_id, first_name, last_name, email, phone, status)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7)`,
		e.EmployeeID, orgID, e.FirstName, e.LastName, e.Email, e.Phone, e.Status)
	return err
}

func (r *EmployeeRepository) Update(ctx context.Context, orgID uuid.UUID, e *models.Employee) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE employees SET first_name=$3, last_name=NULLIF($4,''), email=NULLIF($5,''),
		                     phone=NULLIF($6,''), status=$7, updated_date_utc=now()
		 WHERE organisation_id=$1 AND employee_id=$2`,
		orgID, e.EmployeeID, e.FirstName, e.LastName, e.Email, e.Phone, e.Status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *EmployeeRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.Employee, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT employee_id, first_name, COALESCE(last_name,''), COALESCE(email,''),
		       COALESCE(phone,''), status, updated_date_utc
		  FROM employees WHERE organisation_id=$1 AND status<>'DELETED' ORDER BY first_name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Employee
	for rows.Next() {
		var e models.Employee
		if err := rows.Scan(&e.EmployeeID, &e.FirstName, &e.LastName, &e.Email, &e.Phone, &e.Status, &e.UpdatedDateUTC); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *EmployeeRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Employee, error) {
	var e models.Employee
	err := r.pool.QueryRow(ctx, `
		SELECT employee_id, first_name, COALESCE(last_name,''), COALESCE(email,''),
		       COALESCE(phone,''), status, updated_date_utc
		  FROM employees WHERE organisation_id=$1 AND employee_id=$2`, orgID, id,
	).Scan(&e.EmployeeID, &e.FirstName, &e.LastName, &e.Email, &e.Phone, &e.Status, &e.UpdatedDateUTC)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (r *EmployeeRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE employees SET status='ARCHIVED', updated_date_utc=now()
		  WHERE organisation_id=$1 AND employee_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// Receipts
// -----------------------------------------------------------------------------

type ReceiptRepository struct{ pool *pgxpool.Pool }

func (r *ReceiptRepository) Create(ctx context.Context, orgID uuid.UUID, rc *models.Receipt) error {
	if rc.ReceiptID == uuid.Nil {
		rc.ReceiptID = uuid.New()
	}
	if rc.Status == "" {
		rc.Status = "DRAFT"
	}
	if rc.LineAmountTypes == "" {
		rc.LineAmountTypes = "Exclusive"
	}
	// Totals: recompute from line items only when the caller didn't supply
	// them — matches the tolerant behaviour used by Invoice/PurchaseOrder.
	if rc.Total.IsZero() {
		for _, li := range rc.LineItems {
			rc.SubTotal = rc.SubTotal.Add(li.LineAmount)
			rc.TotalTax = rc.TotalTax.Add(li.TaxAmount)
		}
		rc.Total = rc.SubTotal.Add(rc.TotalTax)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO receipts (receipt_id, organisation_id, user_id, contact_id, date, reference,
		    status, line_amount_types, sub_total, total_tax, total)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11)`,
		rc.ReceiptID, orgID, rc.UserID, rc.ContactID, rc.Date, rc.Reference,
		rc.Status, rc.LineAmountTypes, rc.SubTotal, rc.TotalTax, rc.Total,
	); err != nil {
		return err
	}
	for _, li := range rc.LineItems {
		if _, err := tx.Exec(ctx, `
			INSERT INTO receipt_line_items (receipt_id, description, quantity, unit_amount,
			    account_code, tax_type, tax_amount, line_amount, discount_rate)
			VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9)`,
			rc.ReceiptID, li.Description, li.Quantity, li.UnitAmount,
			li.AccountCode, li.TaxType, li.TaxAmount, li.LineAmount, li.DiscountRate,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *ReceiptRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.Receipt, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT receipt_id, user_id, contact_id, date, COALESCE(reference,''), status,
		       line_amount_types, sub_total, total_tax, total, updated_date_utc
		  FROM receipts WHERE organisation_id=$1 AND status<>'VOIDED' ORDER BY date DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Receipt
	for rows.Next() {
		var rc models.Receipt
		var d *time.Time
		if err := rows.Scan(&rc.ReceiptID, &rc.UserID, &rc.ContactID, &d, &rc.Reference, &rc.Status,
			&rc.LineAmountTypes, &rc.SubTotal, &rc.TotalTax, &rc.Total, &rc.UpdatedDateUTC); err != nil {
			return nil, err
		}
		rc.Date = d
		out = append(out, rc)
	}
	return out, rows.Err()
}

func (r *ReceiptRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Receipt, error) {
	var rc models.Receipt
	var d *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT receipt_id, user_id, contact_id, date, COALESCE(reference,''), status,
		       line_amount_types, sub_total, total_tax, total, updated_date_utc
		  FROM receipts WHERE organisation_id=$1 AND receipt_id=$2`, orgID, id,
	).Scan(&rc.ReceiptID, &rc.UserID, &rc.ContactID, &d, &rc.Reference, &rc.Status,
		&rc.LineAmountTypes, &rc.SubTotal, &rc.TotalTax, &rc.Total, &rc.UpdatedDateUTC)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rc.Date = d
	rows, err := r.pool.Query(ctx, `
		SELECT line_item_id, COALESCE(description,''), quantity, unit_amount,
		       COALESCE(account_code,''), COALESCE(tax_type,''),
		       tax_amount, line_amount, COALESCE(discount_rate,0)
		  FROM receipt_line_items WHERE receipt_id=$1 ORDER BY line_item_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var li models.LineItem
		if err := rows.Scan(&li.LineItemID, &li.Description, &li.Quantity, &li.UnitAmount,
			&li.AccountCode, &li.TaxType, &li.TaxAmount, &li.LineAmount, &li.DiscountRate); err != nil {
			return nil, err
		}
		rc.LineItems = append(rc.LineItems, li)
	}
	return &rc, rows.Err()
}

func (r *ReceiptRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE receipts SET status='VOIDED', updated_date_utc=now()
		  WHERE organisation_id=$1 AND receipt_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// Expense claims
// -----------------------------------------------------------------------------

type ExpenseClaimRepository struct{ pool *pgxpool.Pool }

func (r *ExpenseClaimRepository) Create(ctx context.Context, orgID uuid.UUID, ec *models.ExpenseClaim, receiptIDs []uuid.UUID) error {
	if ec.ExpenseClaimID == uuid.Nil {
		ec.ExpenseClaimID = uuid.New()
	}
	if ec.Status == "" {
		ec.Status = "SUBMITTED"
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO expense_claims (expense_claim_id, organisation_id, user_id, status,
		    payment_due_date, reporting_date, total, amount_due, amount_paid)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		ec.ExpenseClaimID, orgID, ec.UserID, ec.Status,
		ec.PaymentDueDate, ec.ReportingDate, ec.Total, ec.AmountDue, ec.AmountPaid,
	); err != nil {
		return err
	}
	for _, rid := range receiptIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO expense_claim_receipts (expense_claim_id, receipt_id) VALUES ($1,$2)
			 ON CONFLICT DO NOTHING`, ec.ExpenseClaimID, rid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *ExpenseClaimRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.ExpenseClaim, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT expense_claim_id, user_id, status, payment_due_date, reporting_date,
		       total, amount_due, amount_paid, updated_date_utc
		  FROM expense_claims WHERE organisation_id=$1 AND status<>'DELETED' ORDER BY updated_date_utc DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ExpenseClaim
	for rows.Next() {
		var e models.ExpenseClaim
		if err := rows.Scan(&e.ExpenseClaimID, &e.UserID, &e.Status, &e.PaymentDueDate, &e.ReportingDate,
			&e.Total, &e.AmountDue, &e.AmountPaid, &e.UpdatedDateUTC); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *ExpenseClaimRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.ExpenseClaim, error) {
	var e models.ExpenseClaim
	err := r.pool.QueryRow(ctx, `
		SELECT expense_claim_id, user_id, status, payment_due_date, reporting_date,
		       total, amount_due, amount_paid, updated_date_utc
		  FROM expense_claims WHERE organisation_id=$1 AND expense_claim_id=$2`, orgID, id,
	).Scan(&e.ExpenseClaimID, &e.UserID, &e.Status, &e.PaymentDueDate, &e.ReportingDate,
		&e.Total, &e.AmountDue, &e.AmountPaid, &e.UpdatedDateUTC)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (r *ExpenseClaimRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE expense_claims SET status='DELETED', updated_date_utc=now()
		  WHERE organisation_id=$1 AND expense_claim_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
