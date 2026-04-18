package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

type CreditNoteRepository struct {
	pool *pgxpool.Pool
}

const creditNoteColumns = `
	cn.credit_note_id, cn.type, cn.contact_id, COALESCE(c.name,''),
	COALESCE(cn.credit_note_number,''), COALESCE(cn.reference,''),
	cn.status, cn.date, cn.due_date,
	COALESCE(cn.currency_code,''), cn.currency_rate, cn.line_amount_types,
	cn.sub_total, cn.total_tax, cn.total, cn.remaining_credit, cn.updated_date_utc`

const creditNoteFromJoin = ` FROM credit_notes cn LEFT JOIN contacts c ON c.contact_id = cn.contact_id`

func scanCreditNote(row pgx.Row) (*models.CreditNote, error) {
	cn := &models.CreditNote{}
	var (
		contactID   *uuid.UUID
		contactName string
	)
	err := row.Scan(
		&cn.CreditNoteID, &cn.Type, &contactID, &contactName,
		&cn.CreditNoteNumber, &cn.Reference,
		&cn.Status, &cn.Date, &cn.DueDate,
		&cn.CurrencyCode, &cn.CurrencyRate, &cn.LineAmountTypes,
		&cn.SubTotal, &cn.TotalTax, &cn.Total, &cn.RemainingCredit, &cn.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	cn.ContactID = contactID
	if contactID != nil && contactName != "" {
		cn.Contact = &models.Contact{ContactID: *contactID, Name: contactName}
	}
	return cn, nil
}

type CreditNoteFilter struct {
	Type   string
	Status string
}

func (r *CreditNoteRepository) List(ctx context.Context, orgID uuid.UUID, f CreditNoteFilter, p models.Pagination) ([]models.CreditNote, int, error) {
	var sb strings.Builder
	sb.WriteString(creditNoteFromJoin)
	sb.WriteString(" WHERE cn.organisation_id=$1")
	args := []any{orgID}
	if f.Type != "" {
		args = append(args, f.Type)
		sb.WriteString(" AND cn.type=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		sb.WriteString(" AND cn.status=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	where := sb.String()

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*)"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	query := "SELECT " + creditNoteColumns + where +
		" ORDER BY cn.date DESC NULLS LAST, cn.created_at DESC" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.CreditNote
	for rows.Next() {
		cn, err := scanCreditNote(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *cn)
	}
	return out, total, rows.Err()
}

func (r *CreditNoteRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.CreditNote, error) {
	q := "SELECT " + creditNoteColumns + creditNoteFromJoin +
		" WHERE cn.organisation_id=$1 AND cn.credit_note_id=$2"
	cn, err := scanCreditNote(r.pool.QueryRow(ctx, q, orgID, id))
	if err != nil {
		return nil, err
	}
	if err := r.loadLines(ctx, cn); err != nil {
		return nil, err
	}
	if err := r.loadAllocations(ctx, cn); err != nil {
		return nil, err
	}
	return cn, nil
}

func (r *CreditNoteRepository) loadLines(ctx context.Context, cn *models.CreditNote) error {
	rows, err := r.pool.Query(ctx,
		`SELECT line_item_id, COALESCE(description,''), quantity, unit_amount,
			COALESCE(item_code,''), COALESCE(account_code,''), COALESCE(tax_type,''),
			tax_amount, line_amount, discount_rate, discount_amount, sort_order
		 FROM credit_note_line_items WHERE credit_note_id=$1 ORDER BY sort_order, line_item_id`,
		cn.CreditNoteID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var li models.LineItem
		if err := rows.Scan(
			&li.LineItemID, &li.Description, &li.Quantity, &li.UnitAmount,
			&li.ItemCode, &li.AccountCode, &li.TaxType,
			&li.TaxAmount, &li.LineAmount, &li.DiscountRate, &li.DiscountAmount, &li.SortOrder,
		); err != nil {
			return err
		}
		cn.LineItems = append(cn.LineItems, li)
	}
	return rows.Err()
}

func (r *CreditNoteRepository) loadAllocations(ctx context.Context, cn *models.CreditNote) error {
	rows, err := r.pool.Query(ctx,
		`SELECT allocation_id, invoice_id, amount, date
		 FROM credit_note_allocations WHERE credit_note_id=$1 ORDER BY date`,
		cn.CreditNoteID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a models.CreditNoteAllocation
		if err := rows.Scan(&a.AllocationID, &a.InvoiceID, &a.Amount, &a.Date); err != nil {
			return err
		}
		cn.Allocations = append(cn.Allocations, a)
	}
	return rows.Err()
}

func (r *CreditNoteRepository) Create(ctx context.Context, orgID uuid.UUID, cn *models.CreditNote) error {
	recalculateCreditNoteTotals(cn)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `INSERT INTO credit_notes (
		organisation_id, type, contact_id, credit_note_number, reference,
		status, date, due_date, currency_code, currency_rate, line_amount_types,
		sub_total, total_tax, total, remaining_credit
	) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),
		$6,$7,$8, NULLIF($9,''),$10,$11,
		$12,$13,$14,$15)
	  RETURNING credit_note_id, updated_date_utc`,
		orgID, cn.Type, cn.ContactID, cn.CreditNoteNumber, cn.Reference,
		cn.Status, cn.Date, cn.DueDate, cn.CurrencyCode, cn.CurrencyRate, cn.LineAmountTypes,
		cn.SubTotal, cn.TotalTax, cn.Total, cn.Total,
	).Scan(&cn.CreditNoteID, &cn.UpdatedDateUTC); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	cn.RemainingCredit = cn.Total
	if err := writeCreditNoteLines(ctx, tx, cn); err != nil {
		return err
	}
	if cn.Status == models.CreditNoteStatusAuthorised {
		if err := postCreditNoteJournal(ctx, tx, orgID, cn); err != nil {
			return fmt.Errorf("post gl journal: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (r *CreditNoteRepository) Update(ctx context.Context, orgID uuid.UUID, cn *models.CreditNote) error {
	recalculateCreditNoteTotals(cn)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var currentStatus string
	err = tx.QueryRow(ctx,
		`SELECT status FROM credit_notes
		  WHERE organisation_id=$1 AND credit_note_id=$2 FOR UPDATE`,
		orgID, cn.CreditNoteID).Scan(&currentStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if currentStatus == models.CreditNoteStatusVoided ||
		currentStatus == models.CreditNoteStatusDeleted {
		return ErrForbidden
	}
	newStatus := cn.Status
	if newStatus == "" {
		newStatus = currentStatus
	}

	if _, err := tx.Exec(ctx, `UPDATE credit_notes SET
		type=$3, contact_id=$4, credit_note_number=NULLIF($5,''), reference=NULLIF($6,''),
		status=$7, date=$8, due_date=$9, currency_code=NULLIF($10,''), currency_rate=$11,
		line_amount_types=$12,
		sub_total=$13, total_tax=$14, total=$15, remaining_credit=$16,
		updated_date_utc=now()
	  WHERE organisation_id=$1 AND credit_note_id=$2`,
		orgID, cn.CreditNoteID,
		cn.Type, cn.ContactID, cn.CreditNoteNumber, cn.Reference,
		newStatus, cn.Date, cn.DueDate, cn.CurrencyCode, cn.CurrencyRate,
		cn.LineAmountTypes,
		cn.SubTotal, cn.TotalTax, cn.Total, cn.Total,
	); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	cn.Status = newStatus
	cn.RemainingCredit = cn.Total

	if _, err := tx.Exec(ctx,
		`DELETE FROM credit_note_line_items WHERE credit_note_id=$1`, cn.CreditNoteID); err != nil {
		return err
	}
	if err := writeCreditNoteLines(ctx, tx, cn); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM gl_journals WHERE organisation_id=$1 AND source_type='CREDITNOTE' AND source_id=$2`,
		orgID, cn.CreditNoteID); err != nil {
		return err
	}
	if cn.Status == models.CreditNoteStatusAuthorised || cn.Status == models.CreditNoteStatusPaid {
		if err := postCreditNoteJournal(ctx, tx, orgID, cn); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *CreditNoteRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM credit_notes
		  WHERE organisation_id=$1 AND credit_note_id=$2 FOR UPDATE`,
		orgID, id).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == models.CreditNoteStatusPaid ||
		status == models.CreditNoteStatusVoided ||
		status == models.CreditNoteStatusDeleted {
		return ErrForbidden
	}
	target := models.CreditNoteStatusDeleted
	if status == models.CreditNoteStatusAuthorised {
		target = models.CreditNoteStatusVoided
		if _, err := tx.Exec(ctx,
			`DELETE FROM gl_journals
			  WHERE organisation_id=$1 AND source_type='CREDITNOTE' AND source_id=$2`,
			orgID, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE credit_notes SET status=$3, updated_date_utc=now()
		  WHERE organisation_id=$1 AND credit_note_id=$2`, orgID, id, target); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Allocate applies a credit note balance to an invoice, updating both the
// remaining_credit on the note and the amount_paid/amount_due on the invoice.
// Mirrors Xero's `POST /CreditNotes/{id}/Allocations`.
func (r *CreditNoteRepository) Allocate(
	ctx context.Context, orgID uuid.UUID,
	creditNoteID, invoiceID uuid.UUID,
	amount decimal.Decimal, date time.Time,
) error {
	if !amount.IsPositive() {
		return fmt.Errorf("allocation amount must be positive")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		cnStatus    string
		cnRemaining decimal.Decimal
	)
	if err := tx.QueryRow(ctx,
		`SELECT status, remaining_credit
		   FROM credit_notes
		  WHERE organisation_id=$1 AND credit_note_id=$2 FOR UPDATE`,
		orgID, creditNoteID).Scan(&cnStatus, &cnRemaining); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if cnStatus != models.CreditNoteStatusAuthorised {
		return ErrForbidden
	}
	if cnRemaining.LessThan(amount) {
		return fmt.Errorf("amount exceeds remaining credit")
	}
	var invStatus string
	var amountDue decimal.Decimal
	if err := tx.QueryRow(ctx,
		`SELECT status, amount_due FROM invoices
		  WHERE organisation_id=$1 AND invoice_id=$2 FOR UPDATE`,
		orgID, invoiceID).Scan(&invStatus, &amountDue); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if invStatus != models.InvoiceStatusAuthorised {
		return ErrForbidden
	}
	if amountDue.LessThan(amount) {
		amount = amountDue
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO credit_note_allocations (credit_note_id, invoice_id, amount, date)
		 VALUES ($1,$2,$3,$4)`,
		creditNoteID, invoiceID, amount, date); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE credit_notes
		    SET remaining_credit = remaining_credit - $3,
		        status = CASE WHEN remaining_credit - $3 <= 0 THEN 'PAID' ELSE status END,
		        updated_date_utc = now()
		  WHERE organisation_id=$1 AND credit_note_id=$2`,
		orgID, creditNoteID, amount); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE invoices
		    SET amount_credited = amount_credited + $3,
		        amount_due = GREATEST(amount_due - $3, 0),
		        status = CASE WHEN amount_due - $3 <= 0 THEN 'PAID' ELSE status END,
		        updated_date_utc = now()
		  WHERE organisation_id=$1 AND invoice_id=$2`,
		orgID, invoiceID, amount); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func writeCreditNoteLines(ctx context.Context, tx pgx.Tx, cn *models.CreditNote) error {
	for i := range cn.LineItems {
		li := &cn.LineItems[i]
		li.SortOrder = i
		if err := tx.QueryRow(ctx,
			`INSERT INTO credit_note_line_items (
				credit_note_id, sort_order, description, quantity, unit_amount,
				item_code, account_code, tax_type, tax_amount, line_amount,
				discount_rate, discount_amount)
			 VALUES ($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,$10,$11,$12)
			 RETURNING line_item_id`,
			cn.CreditNoteID, li.SortOrder, li.Description, li.Quantity, li.UnitAmount,
			li.ItemCode, li.AccountCode, li.TaxType, li.TaxAmount, li.LineAmount,
			li.DiscountRate, li.DiscountAmount,
		).Scan(&li.LineItemID); err != nil {
			return err
		}
	}
	return nil
}

func recalculateCreditNoteTotals(cn *models.CreditNote) {
	sub := decimal.Zero
	tax := decimal.Zero
	for i := range cn.LineItems {
		li := &cn.LineItems[i]
		if li.Quantity.IsZero() {
			li.Quantity = decimal.NewFromInt(1)
		}
		gross := li.Quantity.Mul(li.UnitAmount)
		lineDiscount := decimal.Zero
		if li.DiscountRate != nil && !li.DiscountRate.IsZero() {
			lineDiscount = gross.Mul(*li.DiscountRate).Div(decimal.NewFromInt(100))
		} else if li.DiscountAmount != nil {
			lineDiscount = *li.DiscountAmount
		}
		li.LineAmount = gross.Sub(lineDiscount)
		sub = sub.Add(li.LineAmount)
		tax = tax.Add(li.TaxAmount)
	}
	cn.SubTotal = sub
	cn.TotalTax = tax
	switch cn.LineAmountTypes {
	case models.LineAmountTypesInclusive:
		cn.Total = sub
	case models.LineAmountTypesNoTax:
		cn.Total = sub
		cn.TotalTax = decimal.Zero
	default:
		cn.Total = sub.Add(tax)
	}
}
