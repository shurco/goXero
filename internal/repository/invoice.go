package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

type InvoiceRepository struct {
	pool *pgxpool.Pool
}

// invoiceColumns projects all invoice columns plus the contact name via LEFT JOIN.
// Using `i.` / `c.` aliases lets us reuse this projection in queries that join
// the contacts table (always, in this repository).
const invoiceColumns = `
	i.invoice_id, i.type, i.contact_id, COALESCE(c.name,''),
	COALESCE(i.invoice_number,''), COALESCE(i.reference,''),
	COALESCE(i.currency_code,''), i.currency_rate, i.status, i.line_amount_types,
	i.date, i.due_date, i.fully_paid_on_date,
	i.sub_total, i.total_tax, i.total, i.total_discount,
	i.amount_due, i.amount_paid, i.amount_credited,
	i.has_attachments, i.sent_to_contact, i.is_discounted, i.updated_date_utc`

const invoiceFromJoin = ` FROM invoices i LEFT JOIN contacts c ON c.contact_id = i.contact_id`

func scanInvoice(row pgx.Row) (*models.Invoice, error) {
	inv := &models.Invoice{}
	var contactID *uuid.UUID
	var contactName string
	err := row.Scan(
		&inv.InvoiceID, &inv.Type, &contactID, &contactName,
		&inv.InvoiceNumber, &inv.Reference,
		&inv.CurrencyCode, &inv.CurrencyRate, &inv.Status, &inv.LineAmountTypes,
		&inv.Date, &inv.DueDate, &inv.FullyPaidOnDate,
		&inv.SubTotal, &inv.TotalTax, &inv.Total, &inv.TotalDiscount,
		&inv.AmountDue, &inv.AmountPaid, &inv.AmountCredited,
		&inv.HasAttachments, &inv.SentToContact, &inv.IsDiscounted, &inv.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	inv.ContactID = contactID
	if contactID != nil && contactName != "" {
		inv.Contact = &models.Contact{ContactID: *contactID, Name: contactName}
	}
	return inv, nil
}

type InvoiceFilter struct {
	Type   string
	Status string
	Search string
}

func (r *InvoiceRepository) List(ctx context.Context, orgID uuid.UUID, f InvoiceFilter, p models.Pagination) ([]models.Invoice, int, error) {
	var sb strings.Builder
	sb.WriteString(invoiceFromJoin)
	sb.WriteString(" WHERE i.organisation_id=$1")
	args := []any{orgID}

	if f.Type != "" {
		args = append(args, f.Type)
		sb.WriteString(" AND i.type=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		sb.WriteString(" AND i.status=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		sb.WriteString(" AND (i.invoice_number ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(" OR i.reference ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(" OR c.name ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(")")
	}
	fromWhere := sb.String()

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*)"+fromWhere, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, p.PageSize, p.Offset())
	query := "SELECT " + invoiceColumns + fromWhere +
		" ORDER BY i.date DESC NULLS LAST, i.created_at DESC" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []models.Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, *inv)
	}
	return list, total, rows.Err()
}

func (r *InvoiceRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Invoice, error) {
	q := "SELECT " + invoiceColumns + invoiceFromJoin +
		" WHERE i.organisation_id=$1 AND i.invoice_id=$2"
	inv, err := scanInvoice(r.pool.QueryRow(ctx, q, orgID, id))
	if err != nil {
		return nil, err
	}
	if err := r.loadLineItems(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

func (r *InvoiceRepository) loadLineItems(ctx context.Context, inv *models.Invoice) error {
	rows, err := r.pool.Query(ctx,
		`SELECT line_item_id, COALESCE(description,''), quantity, unit_amount,
			COALESCE(item_code,''), COALESCE(account_code,''), COALESCE(tax_type,''),
			tax_amount, line_amount, discount_rate, discount_amount, sort_order
		 FROM invoice_line_items WHERE invoice_id=$1 ORDER BY sort_order, line_item_id`, inv.InvoiceID)
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
		inv.LineItems = append(inv.LineItems, li)
	}
	return rows.Err()
}

// Create inserts an invoice + line items inside a transaction, recomputes
// totals and — when the incoming status is AUTHORISED — posts the matching
// GL journal.
func (r *InvoiceRepository) Create(ctx context.Context, orgID uuid.UUID, inv *models.Invoice) error {
	recalculateTotals(inv)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	q := `INSERT INTO invoices (
		organisation_id, type, contact_id, invoice_number, reference,
		currency_code, currency_rate, status, line_amount_types,
		date, due_date,
		sub_total, total_tax, total, total_discount, amount_due
	) VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),
		NULLIF($6,''),$7,$8,$9,
		$10,$11,
		$12,$13,$14,$15,$16)
	RETURNING invoice_id, updated_date_utc`
	err = tx.QueryRow(ctx, q,
		orgID, inv.Type, inv.ContactID, inv.InvoiceNumber, inv.Reference,
		inv.CurrencyCode, inv.CurrencyRate, inv.Status, inv.LineAmountTypes,
		inv.Date, inv.DueDate,
		inv.SubTotal, inv.TotalTax, inv.Total, inv.TotalDiscount, inv.Total,
	).Scan(&inv.InvoiceID, &inv.UpdatedDateUTC)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("insert invoice: %w", err)
	}
	inv.AmountDue = inv.Total

	if err := writeInvoiceLines(ctx, tx, inv); err != nil {
		return err
	}

	if inv.Status == models.InvoiceStatusAuthorised {
		if err := postInvoiceJournal(ctx, tx, orgID, inv); err != nil {
			return fmt.Errorf("post gl journal: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func writeInvoiceLines(ctx context.Context, tx pgx.Tx, inv *models.Invoice) error {
	for i := range inv.LineItems {
		li := &inv.LineItems[i]
		li.SortOrder = i
		qLine := `INSERT INTO invoice_line_items (
			invoice_id, sort_order, description, quantity, unit_amount,
			item_code, account_code, tax_type, tax_amount, line_amount,
			discount_rate, discount_amount
		) VALUES ($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,$10,$11,$12)
		RETURNING line_item_id`
		if err := tx.QueryRow(ctx, qLine,
			inv.InvoiceID, li.SortOrder, li.Description, li.Quantity, li.UnitAmount,
			li.ItemCode, li.AccountCode, li.TaxType, li.TaxAmount, li.LineAmount,
			li.DiscountRate, li.DiscountAmount,
		).Scan(&li.LineItemID); err != nil {
			return fmt.Errorf("insert line item: %w", err)
		}
	}
	return nil
}

// Update replaces editable header fields AND the full line item list.
// Totals are recomputed; amount_due is kept consistent with amount_paid.
// Not allowed once the invoice is PAID/VOIDED/DELETED.
func (r *InvoiceRepository) Update(ctx context.Context, orgID uuid.UUID, inv *models.Invoice) error {
	recalculateTotals(inv)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var currentStatus string
	var amountPaid decimal.Decimal
	err = tx.QueryRow(ctx,
		`SELECT status, amount_paid FROM invoices
		 WHERE organisation_id=$1 AND invoice_id=$2 FOR UPDATE`,
		orgID, inv.InvoiceID).Scan(&currentStatus, &amountPaid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if currentStatus == models.InvoiceStatusVoided || currentStatus == models.InvoiceStatusDeleted {
		return ErrForbidden
	}
	newStatus := inv.Status
	if newStatus == "" {
		newStatus = currentStatus
	}

	amountDue := inv.Total.Sub(amountPaid)
	if amountDue.IsNegative() {
		amountDue = decimal.Zero
	}
	autoPaid := !amountPaid.IsZero() && amountDue.IsZero()
	if autoPaid {
		newStatus = models.InvoiceStatusPaid
	}

	q := `UPDATE invoices SET
		type=$3, contact_id=$4,
		invoice_number=NULLIF($5,''), reference=NULLIF($6,''),
		currency_code=NULLIF($7,''), currency_rate=$8,
		status=$9, line_amount_types=$10,
		date=$11, due_date=$12,
		sub_total=$13, total_tax=$14, total=$15, total_discount=$16, amount_due=$17,
		updated_date_utc=now()
	  WHERE organisation_id=$1 AND invoice_id=$2
	  RETURNING updated_date_utc`
	err = tx.QueryRow(ctx, q,
		orgID, inv.InvoiceID,
		inv.Type, inv.ContactID,
		inv.InvoiceNumber, inv.Reference,
		inv.CurrencyCode, inv.CurrencyRate,
		newStatus, inv.LineAmountTypes,
		inv.Date, inv.DueDate,
		inv.SubTotal, inv.TotalTax, inv.Total, inv.TotalDiscount, amountDue,
	).Scan(&inv.UpdatedDateUTC)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	inv.AmountDue = amountDue
	inv.AmountPaid = amountPaid
	inv.Status = newStatus

	if _, err := tx.Exec(ctx,
		`DELETE FROM invoice_line_items WHERE invoice_id=$1`, inv.InvoiceID); err != nil {
		return err
	}
	if err := writeInvoiceLines(ctx, tx, inv); err != nil {
		return err
	}

	// Reposting journal: we take the simple path — delete the previously
	// posted journal and, if the invoice is now AUTHORISED/PAID, write a new
	// one reflecting the latest totals. Matches how Xero regenerates journals
	// on edit.
	if _, err := tx.Exec(ctx,
		`DELETE FROM gl_journals WHERE organisation_id=$1 AND source_type='INVOICE' AND source_id=$2`,
		orgID, inv.InvoiceID); err != nil {
		return err
	}
	if inv.Status == models.InvoiceStatusAuthorised || inv.Status == models.InvoiceStatusPaid {
		if err := postInvoiceJournal(ctx, tx, orgID, inv); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Delete is really a soft-delete: DRAFT/SUBMITTED invoices become DELETED;
// AUTHORISED invoices become VOIDED (and their GL entry is reversed).
// PAID/VOIDED/DELETED invoices cannot be touched again.
func (r *InvoiceRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx,
		`SELECT status FROM invoices
		 WHERE organisation_id=$1 AND invoice_id=$2 FOR UPDATE`,
		orgID, id).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	switch status {
	case models.InvoiceStatusPaid,
		models.InvoiceStatusVoided,
		models.InvoiceStatusDeleted:
		return ErrForbidden
	}
	target := models.InvoiceStatusDeleted
	if status == models.InvoiceStatusAuthorised {
		target = models.InvoiceStatusVoided
		if _, err := tx.Exec(ctx,
			`DELETE FROM gl_journals WHERE organisation_id=$1 AND source_type='INVOICE' AND source_id=$2`,
			orgID, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE invoices SET status=$3, updated_date_utc=now()
		 WHERE organisation_id=$1 AND invoice_id=$2`, orgID, id, target); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// recalculateTotals applies simple totals rollup assuming Exclusive amounts.
// For Inclusive / NoTax the UI is expected to send the correct TaxAmount.
func recalculateTotals(inv *models.Invoice) {
	sub := decimal.Zero
	tax := decimal.Zero
	discount := decimal.Zero

	for i := range inv.LineItems {
		li := &inv.LineItems[i]
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
		netLine := gross.Sub(lineDiscount)
		li.LineAmount = netLine

		sub = sub.Add(netLine)
		tax = tax.Add(li.TaxAmount)
		discount = discount.Add(lineDiscount)
	}

	inv.SubTotal = sub
	inv.TotalTax = tax
	inv.TotalDiscount = discount

	switch inv.LineAmountTypes {
	case models.LineAmountTypesInclusive:
		inv.Total = sub
	case models.LineAmountTypesNoTax:
		inv.Total = sub
		inv.TotalTax = decimal.Zero
	default:
		inv.Total = sub.Add(tax)
	}
}

// UpdateStatus implements the Xero-compatible whitelist of transitions:
//
//	DRAFT     -> SUBMITTED, AUTHORISED, DELETED
//	SUBMITTED -> DRAFT, AUTHORISED, DELETED
//	AUTHORISED-> VOIDED, PAID (auto)
//	PAID/VOIDED/DELETED are terminal.
//
// Transitions into AUTHORISED post a GL journal; leaving AUTHORISED via
// VOIDED removes it.
func (r *InvoiceRepository) UpdateStatus(ctx context.Context, orgID, id uuid.UUID, status string) error {
	if !validInvoiceStatus(status) {
		return fmt.Errorf("invalid status")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var current string
	err = tx.QueryRow(ctx,
		`SELECT status FROM invoices
		 WHERE organisation_id=$1 AND invoice_id=$2 FOR UPDATE`, orgID, id).Scan(&current)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if !invoiceTransitionAllowed(current, status) {
		return ErrForbidden
	}

	if _, err := tx.Exec(ctx,
		`UPDATE invoices SET status=$3, updated_date_utc=now()
		 WHERE organisation_id=$1 AND invoice_id=$2`, orgID, id, status); err != nil {
		return err
	}

	switch {
	case current != models.InvoiceStatusAuthorised && status == models.InvoiceStatusAuthorised:
		inv, err := loadInvoiceTx(ctx, tx, orgID, id)
		if err != nil {
			return err
		}
		if err := postInvoiceJournal(ctx, tx, orgID, inv); err != nil {
			return err
		}
	case current == models.InvoiceStatusAuthorised && (status == models.InvoiceStatusVoided || status == models.InvoiceStatusDeleted):
		if _, err := tx.Exec(ctx,
			`DELETE FROM gl_journals
			 WHERE organisation_id=$1 AND source_type='INVOICE' AND source_id=$2`,
			orgID, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func validInvoiceStatus(s string) bool {
	switch s {
	case models.InvoiceStatusDraft,
		models.InvoiceStatusSubmitted,
		models.InvoiceStatusAuthorised,
		models.InvoiceStatusPaid,
		models.InvoiceStatusVoided,
		models.InvoiceStatusDeleted:
		return true
	}
	return false
}

func invoiceTransitionAllowed(from, to string) bool {
	if from == to {
		return true
	}
	allowed := map[string][]string{
		models.InvoiceStatusDraft:      {models.InvoiceStatusSubmitted, models.InvoiceStatusAuthorised, models.InvoiceStatusDeleted},
		models.InvoiceStatusSubmitted:  {models.InvoiceStatusDraft, models.InvoiceStatusAuthorised, models.InvoiceStatusDeleted},
		models.InvoiceStatusAuthorised: {models.InvoiceStatusVoided, models.InvoiceStatusPaid},
	}
	for _, t := range allowed[from] {
		if t == to {
			return true
		}
	}
	return false
}

// loadInvoiceTx is a minimal loader used by GL posting while inside a write tx.
func loadInvoiceTx(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) (*models.Invoice, error) {
	row := tx.QueryRow(ctx,
		"SELECT "+invoiceColumns+invoiceFromJoin+
			" WHERE i.organisation_id=$1 AND i.invoice_id=$2", orgID, id)
	inv, err := scanInvoice(row)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx,
		`SELECT line_item_id, COALESCE(description,''), quantity, unit_amount,
			COALESCE(item_code,''), COALESCE(account_code,''), COALESCE(tax_type,''),
			tax_amount, line_amount, discount_rate, discount_amount, sort_order
		 FROM invoice_line_items WHERE invoice_id=$1 ORDER BY sort_order`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var li models.LineItem
		if err := rows.Scan(
			&li.LineItemID, &li.Description, &li.Quantity, &li.UnitAmount,
			&li.ItemCode, &li.AccountCode, &li.TaxType,
			&li.TaxAmount, &li.LineAmount, &li.DiscountRate, &li.DiscountAmount, &li.SortOrder,
		); err != nil {
			return nil, err
		}
		inv.LineItems = append(inv.LineItems, li)
	}
	return inv, rows.Err()
}

// Summary is used by the dashboard.
type Summary struct {
	TotalInvoices int
	Draft         int
	Authorised    int
	Paid          int
	Overdue       int
	TotalDue      decimal.Decimal
	TotalPaid     decimal.Decimal
}

func (r *InvoiceRepository) Summary(ctx context.Context, orgID uuid.UUID) (*Summary, error) {
	s := &Summary{}
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status='DRAFT'),
			COUNT(*) FILTER (WHERE status='AUTHORISED'),
			COUNT(*) FILTER (WHERE status='PAID'),
			COUNT(*) FILTER (WHERE status='AUTHORISED' AND due_date < CURRENT_DATE),
			COALESCE(SUM(amount_due),0),
			COALESCE(SUM(amount_paid),0)
		FROM invoices WHERE organisation_id=$1`, orgID).
		Scan(&s.TotalInvoices, &s.Draft, &s.Authorised, &s.Paid, &s.Overdue, &s.TotalDue, &s.TotalPaid)
	if err != nil {
		return nil, err
	}
	return s, nil
}
