package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

type QuoteRepository struct {
	pool *pgxpool.Pool
}

const quoteColumns = `
	q.quote_id, q.contact_id, COALESCE(c.name,''),
	COALESCE(q.quote_number,''), COALESCE(q.reference,''),
	COALESCE(q.title,''), COALESCE(q.summary,''), COALESCE(q.terms,''),
	q.date, q.expiry_date, q.status, q.line_amount_types,
	COALESCE(q.currency_code,''), q.currency_rate, q.branding_theme_id,
	q.sub_total, q.total_tax, q.total, q.total_discount, q.updated_date_utc`

const quoteFromJoin = ` FROM quotes q LEFT JOIN contacts c ON c.contact_id = q.contact_id`

func scanQuote(row pgx.Row) (*models.Quote, error) {
	qm := &models.Quote{}
	var (
		contactID   *uuid.UUID
		contactName string
	)
	err := row.Scan(
		&qm.QuoteID, &contactID, &contactName,
		&qm.QuoteNumber, &qm.Reference,
		&qm.Title, &qm.Summary, &qm.Terms,
		&qm.Date, &qm.ExpiryDate, &qm.Status, &qm.LineAmountTypes,
		&qm.CurrencyCode, &qm.CurrencyRate, &qm.BrandingThemeID,
		&qm.SubTotal, &qm.TotalTax, &qm.Total, &qm.TotalDiscount, &qm.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	qm.ContactID = contactID
	if contactID != nil && contactName != "" {
		qm.Contact = &models.Contact{ContactID: *contactID, Name: contactName}
	}
	return qm, nil
}

type QuoteFilter struct {
	Status string
}

func (r *QuoteRepository) List(ctx context.Context, orgID uuid.UUID, f QuoteFilter, p models.Pagination) ([]models.Quote, int, error) {
	var sb strings.Builder
	sb.WriteString(quoteFromJoin)
	sb.WriteString(" WHERE q.organisation_id=$1")
	args := []any{orgID}
	if f.Status != "" {
		args = append(args, f.Status)
		sb.WriteString(" AND q.status=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	where := sb.String()

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*)"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	sqlText := "SELECT " + quoteColumns + where +
		" ORDER BY q.date DESC NULLS LAST, q.created_at DESC" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.Quote
	for rows.Next() {
		q, err := scanQuote(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *q)
	}
	return out, total, rows.Err()
}

func (r *QuoteRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Quote, error) {
	sqlText := "SELECT " + quoteColumns + quoteFromJoin +
		" WHERE q.organisation_id=$1 AND q.quote_id=$2"
	q, err := scanQuote(r.pool.QueryRow(ctx, sqlText, orgID, id))
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT line_item_id, COALESCE(description,''), quantity, unit_amount,
			COALESCE(item_code,''), COALESCE(account_code,''), COALESCE(tax_type,''),
			tax_amount, line_amount, discount_rate, sort_order
		 FROM quote_line_items WHERE quote_id=$1 ORDER BY sort_order`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var li models.LineItem
		if err := rows.Scan(&li.LineItemID, &li.Description, &li.Quantity, &li.UnitAmount,
			&li.ItemCode, &li.AccountCode, &li.TaxType,
			&li.TaxAmount, &li.LineAmount, &li.DiscountRate, &li.SortOrder); err != nil {
			return nil, err
		}
		q.LineItems = append(q.LineItems, li)
	}
	return q, rows.Err()
}

func (r *QuoteRepository) Create(ctx context.Context, orgID uuid.UUID, qm *models.Quote) error {
	recalculateQuoteTotals(qm)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `INSERT INTO quotes (
		organisation_id, contact_id, quote_number, reference,
		title, summary, terms, date, expiry_date,
		currency_code, currency_rate, status, line_amount_types,
		sub_total, total_tax, total, total_discount, branding_theme_id
	) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),
		NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,
		NULLIF($10,''),$11,$12,$13,
		$14,$15,$16,$17,$18)
	  RETURNING quote_id, updated_date_utc`,
		orgID, qm.ContactID, qm.QuoteNumber, qm.Reference,
		qm.Title, qm.Summary, qm.Terms, qm.Date, qm.ExpiryDate,
		qm.CurrencyCode, qm.CurrencyRate, qm.Status, qm.LineAmountTypes,
		qm.SubTotal, qm.TotalTax, qm.Total, qm.TotalDiscount, qm.BrandingThemeID,
	).Scan(&qm.QuoteID, &qm.UpdatedDateUTC); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	if err := writeQuoteLines(ctx, tx, qm); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *QuoteRepository) Update(ctx context.Context, orgID uuid.UUID, qm *models.Quote) error {
	recalculateQuoteTotals(qm)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	cmd, err := tx.Exec(ctx, `UPDATE quotes SET
		contact_id=$3, quote_number=NULLIF($4,''), reference=NULLIF($5,''),
		title=NULLIF($6,''), summary=NULLIF($7,''), terms=NULLIF($8,''),
		date=$9, expiry_date=$10,
		currency_code=NULLIF($11,''), currency_rate=$12,
		status=$13, line_amount_types=$14,
		sub_total=$15, total_tax=$16, total=$17, total_discount=$18, branding_theme_id=$19,
		updated_date_utc=now()
	  WHERE organisation_id=$1 AND quote_id=$2`,
		orgID, qm.QuoteID, qm.ContactID, qm.QuoteNumber, qm.Reference,
		qm.Title, qm.Summary, qm.Terms, qm.Date, qm.ExpiryDate,
		qm.CurrencyCode, qm.CurrencyRate, qm.Status, qm.LineAmountTypes,
		qm.SubTotal, qm.TotalTax, qm.Total, qm.TotalDiscount, qm.BrandingThemeID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM quote_line_items WHERE quote_id=$1`, qm.QuoteID); err != nil {
		return err
	}
	if err := writeQuoteLines(ctx, tx, qm); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func writeQuoteLines(ctx context.Context, tx pgx.Tx, qm *models.Quote) error {
	for i := range qm.LineItems {
		li := &qm.LineItems[i]
		li.SortOrder = i
		if err := tx.QueryRow(ctx,
			`INSERT INTO quote_line_items (
				quote_id, sort_order, description, quantity, unit_amount,
				item_code, account_code, tax_type, tax_amount, line_amount, discount_rate)
			 VALUES ($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,$10,$11)
			 RETURNING line_item_id`,
			qm.QuoteID, li.SortOrder, li.Description, li.Quantity, li.UnitAmount,
			li.ItemCode, li.AccountCode, li.TaxType, li.TaxAmount, li.LineAmount, li.DiscountRate,
		).Scan(&li.LineItemID); err != nil {
			return err
		}
	}
	return nil
}

func (r *QuoteRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE quotes SET status='DELETED', updated_date_utc=now()
		 WHERE organisation_id=$1 AND quote_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func recalculateQuoteTotals(qm *models.Quote) {
	sub := decimal.Zero
	tax := decimal.Zero
	disc := decimal.Zero
	for i := range qm.LineItems {
		li := &qm.LineItems[i]
		if li.Quantity.IsZero() {
			li.Quantity = decimal.NewFromInt(1)
		}
		gross := li.Quantity.Mul(li.UnitAmount)
		lineDiscount := decimal.Zero
		if li.DiscountRate != nil && !li.DiscountRate.IsZero() {
			lineDiscount = gross.Mul(*li.DiscountRate).Div(decimal.NewFromInt(100))
		}
		li.LineAmount = gross.Sub(lineDiscount)
		sub = sub.Add(li.LineAmount)
		tax = tax.Add(li.TaxAmount)
		disc = disc.Add(lineDiscount)
	}
	qm.SubTotal = sub
	qm.TotalTax = tax
	qm.TotalDiscount = disc
	switch qm.LineAmountTypes {
	case models.LineAmountTypesInclusive:
		qm.Total = sub
	case models.LineAmountTypesNoTax:
		qm.Total = sub
		qm.TotalTax = decimal.Zero
	default:
		qm.Total = sub.Add(tax)
	}
}
