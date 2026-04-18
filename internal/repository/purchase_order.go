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

type PurchaseOrderRepository struct {
	pool *pgxpool.Pool
}

const poColumns = `
	p.purchase_order_id, p.contact_id, COALESCE(c.name,''),
	COALESCE(p.purchase_order_number,''), COALESCE(p.reference,''),
	p.date, p.delivery_date, COALESCE(p.delivery_address,''),
	COALESCE(p.attention_to,''), COALESCE(p.telephone,''), COALESCE(p.delivery_instructions,''),
	p.status, p.line_amount_types,
	COALESCE(p.currency_code,''), p.currency_rate,
	p.sub_total, p.total_tax, p.total, p.updated_date_utc`

const poFromJoin = ` FROM purchase_orders p LEFT JOIN contacts c ON c.contact_id = p.contact_id`

func scanPO(row pgx.Row) (*models.PurchaseOrder, error) {
	po := &models.PurchaseOrder{}
	var (
		contactID *uuid.UUID
		cname     string
	)
	err := row.Scan(
		&po.PurchaseOrderID, &contactID, &cname,
		&po.PurchaseOrderNumber, &po.Reference,
		&po.Date, &po.DeliveryDate, &po.DeliveryAddress,
		&po.AttentionTo, &po.Telephone, &po.DeliveryInstructions,
		&po.Status, &po.LineAmountTypes,
		&po.CurrencyCode, &po.CurrencyRate,
		&po.SubTotal, &po.TotalTax, &po.Total, &po.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	po.ContactID = contactID
	if contactID != nil && cname != "" {
		po.Contact = &models.Contact{ContactID: *contactID, Name: cname}
	}
	return po, nil
}

type PurchaseOrderFilter struct {
	Status string
}

func (r *PurchaseOrderRepository) List(ctx context.Context, orgID uuid.UUID, f PurchaseOrderFilter, p models.Pagination) ([]models.PurchaseOrder, int, error) {
	var sb strings.Builder
	sb.WriteString(poFromJoin)
	sb.WriteString(" WHERE p.organisation_id=$1")
	args := []any{orgID}
	if f.Status != "" {
		args = append(args, f.Status)
		sb.WriteString(" AND p.status=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	where := sb.String()

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*)"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	q := "SELECT " + poColumns + where +
		" ORDER BY p.date DESC NULLS LAST, p.created_at DESC" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.PurchaseOrder
	for rows.Next() {
		po, err := scanPO(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *po)
	}
	return out, total, rows.Err()
}

func (r *PurchaseOrderRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.PurchaseOrder, error) {
	po, err := scanPO(r.pool.QueryRow(ctx, "SELECT "+poColumns+poFromJoin+
		" WHERE p.organisation_id=$1 AND p.purchase_order_id=$2", orgID, id))
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT line_item_id, COALESCE(description,''), quantity, unit_amount,
			COALESCE(item_code,''), COALESCE(account_code,''), COALESCE(tax_type,''),
			tax_amount, line_amount, sort_order
		 FROM purchase_order_line_items WHERE purchase_order_id=$1 ORDER BY sort_order`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var li models.LineItem
		if err := rows.Scan(&li.LineItemID, &li.Description, &li.Quantity, &li.UnitAmount,
			&li.ItemCode, &li.AccountCode, &li.TaxType,
			&li.TaxAmount, &li.LineAmount, &li.SortOrder); err != nil {
			return nil, err
		}
		po.LineItems = append(po.LineItems, li)
	}
	return po, rows.Err()
}

func (r *PurchaseOrderRepository) Create(ctx context.Context, orgID uuid.UUID, po *models.PurchaseOrder) error {
	recalculatePOTotals(po)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `INSERT INTO purchase_orders (
		organisation_id, contact_id, purchase_order_number, reference,
		date, delivery_date, delivery_address, attention_to, telephone, delivery_instructions,
		currency_code, currency_rate, status, line_amount_types,
		sub_total, total_tax, total
	) VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),
		$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),
		NULLIF($11,''),$12,$13,$14,$15,$16,$17)
	  RETURNING purchase_order_id, updated_date_utc`,
		orgID, po.ContactID, po.PurchaseOrderNumber, po.Reference,
		po.Date, po.DeliveryDate, po.DeliveryAddress, po.AttentionTo, po.Telephone, po.DeliveryInstructions,
		po.CurrencyCode, po.CurrencyRate, po.Status, po.LineAmountTypes,
		po.SubTotal, po.TotalTax, po.Total,
	).Scan(&po.PurchaseOrderID, &po.UpdatedDateUTC); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	if err := writePOLines(ctx, tx, po); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PurchaseOrderRepository) Update(ctx context.Context, orgID uuid.UUID, po *models.PurchaseOrder) error {
	recalculatePOTotals(po)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	cmd, err := tx.Exec(ctx, `UPDATE purchase_orders SET
		contact_id=$3, purchase_order_number=NULLIF($4,''), reference=NULLIF($5,''),
		date=$6, delivery_date=$7, delivery_address=NULLIF($8,''),
		attention_to=NULLIF($9,''), telephone=NULLIF($10,''), delivery_instructions=NULLIF($11,''),
		currency_code=NULLIF($12,''), currency_rate=$13, status=$14, line_amount_types=$15,
		sub_total=$16, total_tax=$17, total=$18,
		updated_date_utc=now()
	  WHERE organisation_id=$1 AND purchase_order_id=$2`,
		orgID, po.PurchaseOrderID, po.ContactID, po.PurchaseOrderNumber, po.Reference,
		po.Date, po.DeliveryDate, po.DeliveryAddress, po.AttentionTo, po.Telephone, po.DeliveryInstructions,
		po.CurrencyCode, po.CurrencyRate, po.Status, po.LineAmountTypes,
		po.SubTotal, po.TotalTax, po.Total)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM purchase_order_line_items WHERE purchase_order_id=$1`, po.PurchaseOrderID); err != nil {
		return err
	}
	if err := writePOLines(ctx, tx, po); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PurchaseOrderRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE purchase_orders SET status='DELETED', updated_date_utc=now()
		 WHERE organisation_id=$1 AND purchase_order_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func writePOLines(ctx context.Context, tx pgx.Tx, po *models.PurchaseOrder) error {
	for i := range po.LineItems {
		li := &po.LineItems[i]
		li.SortOrder = i
		if err := tx.QueryRow(ctx,
			`INSERT INTO purchase_order_line_items (
				purchase_order_id, sort_order, description, quantity, unit_amount,
				item_code, account_code, tax_type, tax_amount, line_amount)
			 VALUES ($1,$2,NULLIF($3,''),$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,$10)
			 RETURNING line_item_id`,
			po.PurchaseOrderID, li.SortOrder, li.Description, li.Quantity, li.UnitAmount,
			li.ItemCode, li.AccountCode, li.TaxType, li.TaxAmount, li.LineAmount,
		).Scan(&li.LineItemID); err != nil {
			return err
		}
	}
	return nil
}

func recalculatePOTotals(po *models.PurchaseOrder) {
	sub := decimal.Zero
	tax := decimal.Zero
	for i := range po.LineItems {
		li := &po.LineItems[i]
		if li.Quantity.IsZero() {
			li.Quantity = decimal.NewFromInt(1)
		}
		li.LineAmount = li.Quantity.Mul(li.UnitAmount)
		sub = sub.Add(li.LineAmount)
		tax = tax.Add(li.TaxAmount)
	}
	po.SubTotal = sub
	po.TotalTax = tax
	switch po.LineAmountTypes {
	case models.LineAmountTypesInclusive:
		po.Total = sub
	case models.LineAmountTypesNoTax:
		po.Total = sub
		po.TotalTax = decimal.Zero
	default:
		po.Total = sub.Add(tax)
	}
}
