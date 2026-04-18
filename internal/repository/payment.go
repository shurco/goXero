package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

type PaymentRepository struct {
	pool *pgxpool.Pool
}

const paymentColumns = `
	payment_id, invoice_id, credit_note_id, account_id,
	payment_type, status, date, currency_rate, amount,
	COALESCE(reference,''), is_reconciled, updated_date_utc`

func scanPayment(row pgx.Row) (*models.Payment, error) {
	p := &models.Payment{}
	err := row.Scan(
		&p.PaymentID, &p.InvoiceID, &p.CreditNoteID, &p.AccountID,
		&p.PaymentType, &p.Status, &p.Date, &p.CurrencyRate, &p.Amount,
		&p.Reference, &p.IsReconciled, &p.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

func (r *PaymentRepository) List(ctx context.Context, orgID uuid.UUID, p models.Pagination) ([]models.Payment, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM payments WHERE organisation_id=$1`, orgID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx,
		"SELECT "+paymentColumns+
			" FROM payments WHERE organisation_id=$1 ORDER BY date DESC LIMIT $2 OFFSET $3",
		orgID, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []models.Payment
	for rows.Next() {
		pm, err := scanPayment(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *pm)
	}
	return out, total, rows.Err()
}

func (r *PaymentRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Payment, error) {
	q := "SELECT " + paymentColumns +
		" FROM payments WHERE organisation_id=$1 AND payment_id=$2"
	return scanPayment(r.pool.QueryRow(ctx, q, orgID, id))
}

// Create records a payment and applies it to an invoice.
// The invoice (if provided) MUST belong to orgID — otherwise we refuse to
// proceed to prevent tenant-crossing payments.
func (r *PaymentRepository) Create(ctx context.Context, orgID uuid.UUID, p *models.Payment) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if p.InvoiceID != nil {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(
				SELECT 1 FROM invoices WHERE invoice_id=$1 AND organisation_id=$2
			)`, *p.InvoiceID, orgID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}

	q := `INSERT INTO payments (
		organisation_id, invoice_id, credit_note_id, account_id,
		payment_type, status, date, currency_rate, amount, reference
	) VALUES ($1,$2,$3,$4,$5,COALESCE(NULLIF($6,''),'AUTHORISED'),$7,$8,$9,NULLIF($10,''))
	RETURNING payment_id, updated_date_utc`
	if err := tx.QueryRow(ctx, q,
		orgID, p.InvoiceID, p.CreditNoteID, p.AccountID,
		p.PaymentType, p.Status, p.Date, p.CurrencyRate, p.Amount, p.Reference,
	).Scan(&p.PaymentID, &p.UpdatedDateUTC); err != nil {
		return err
	}

	if p.InvoiceID != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE invoices
			 SET amount_paid = amount_paid + $3,
			     amount_due  = GREATEST(amount_due - $3, 0),
			     status = CASE WHEN amount_due - $3 <= 0 THEN 'PAID' ELSE status END,
			     fully_paid_on_date = CASE WHEN amount_due - $3 <= 0 THEN $4::date ELSE fully_paid_on_date END,
			     updated_date_utc = now()
			 WHERE organisation_id=$1 AND invoice_id=$2`,
			orgID, *p.InvoiceID, p.Amount, p.Date); err != nil {
			return err
		}
	}

	if err := postPaymentJournal(ctx, tx, orgID, p); err != nil {
		return fmt.Errorf("post gl journal: %w", err)
	}
	return tx.Commit(ctx)
}

// Void marks a payment as voided and reverses its effect on the invoice.
// It also writes a reversing GL journal.
func (r *PaymentRepository) Void(ctx context.Context, orgID, id uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		status string
		invID  *uuid.UUID
		amount decimal.Decimal
	)
	err = tx.QueryRow(ctx,
		`SELECT status, invoice_id, amount
		   FROM payments WHERE organisation_id=$1 AND payment_id=$2 FOR UPDATE`,
		orgID, id).Scan(&status, &invID, &amount)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == "VOIDED" {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE payments SET status='VOIDED', updated_date_utc=now()
		 WHERE organisation_id=$1 AND payment_id=$2`, orgID, id); err != nil {
		return err
	}
	if invID != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE invoices
			 SET amount_paid = GREATEST(amount_paid - $3, 0),
			     amount_due  = amount_due + $3,
			     status      = CASE WHEN status='PAID' THEN 'AUTHORISED' ELSE status END,
			     fully_paid_on_date = NULL,
			     updated_date_utc = now()
			 WHERE organisation_id=$1 AND invoice_id=$2`,
			orgID, *invID, amount); err != nil {
			return err
		}
	}
	if err := postPaymentReversal(ctx, tx, orgID, id); err != nil {
		return fmt.Errorf("reverse gl journal: %w", err)
	}
	return tx.Commit(ctx)
}

// ListForInvoice returns every payment recorded against the invoice.
func (r *PaymentRepository) ListForInvoice(ctx context.Context, orgID, invoiceID uuid.UUID) ([]models.Payment, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT "+paymentColumns+
			" FROM payments WHERE organisation_id=$1 AND invoice_id=$2 ORDER BY date DESC",
		orgID, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Payment
	for rows.Next() {
		pm, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *pm)
	}
	return out, rows.Err()
}
