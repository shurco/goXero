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

type BankTransactionRepository struct {
	pool *pgxpool.Pool
}

const bankTxColumns = `
	b.bank_transaction_id, b.type, b.contact_id, COALESCE(c.name,''),
	b.bank_account_id, COALESCE(a.name,''), COALESCE(a.code,''),
	b.is_reconciled, b.date, COALESCE(b.reference,''),
	COALESCE(b.currency_code,''), b.currency_rate, COALESCE(b.url,''),
	b.status, b.line_amount_types,
	b.sub_total, b.total_tax, b.total, b.updated_date_utc`

const bankTxFromJoin = ` FROM bank_transactions b
	LEFT JOIN contacts c ON c.contact_id = b.contact_id
	LEFT JOIN accounts a ON a.account_id = b.bank_account_id`

func scanBankTx(row pgx.Row) (*models.BankTransaction, error) {
	bt := &models.BankTransaction{}
	var (
		contactID *uuid.UUID
		contactNm string
		bankID    *uuid.UUID
		bankNm    string
		bankCode  string
	)
	err := row.Scan(
		&bt.BankTransactionID, &bt.Type, &contactID, &contactNm,
		&bankID, &bankNm, &bankCode,
		&bt.IsReconciled, &bt.Date, &bt.Reference,
		&bt.CurrencyCode, &bt.CurrencyRate, &bt.URL,
		&bt.Status, &bt.LineAmountTypes,
		&bt.SubTotal, &bt.TotalTax, &bt.Total, &bt.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	bt.ContactID = contactID
	if contactID != nil && contactNm != "" {
		bt.Contact = &models.Contact{ContactID: *contactID, Name: contactNm}
	}
	bt.BankAccountID = bankID
	if bankID != nil {
		bt.BankAccount = &models.Account{AccountID: *bankID, Name: bankNm, Code: bankCode}
	}
	return bt, nil
}

type BankTransactionFilter struct {
	Type   string
	Status string
}

func (r *BankTransactionRepository) List(ctx context.Context, orgID uuid.UUID, f BankTransactionFilter, p models.Pagination) ([]models.BankTransaction, int, error) {
	var sb strings.Builder
	sb.WriteString(bankTxFromJoin)
	sb.WriteString(" WHERE b.organisation_id=$1")
	args := []any{orgID}
	if f.Type != "" {
		args = append(args, f.Type)
		sb.WriteString(" AND b.type=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		sb.WriteString(" AND b.status=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	where := sb.String()

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*)"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	q := "SELECT " + bankTxColumns + where +
		" ORDER BY b.date DESC NULLS LAST" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.BankTransaction
	for rows.Next() {
		bt, err := scanBankTx(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *bt)
	}
	return out, total, rows.Err()
}

func (r *BankTransactionRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.BankTransaction, error) {
	q := "SELECT " + bankTxColumns + bankTxFromJoin +
		" WHERE b.organisation_id=$1 AND b.bank_transaction_id=$2"
	bt, err := scanBankTx(r.pool.QueryRow(ctx, q, orgID, id))
	if err != nil {
		return nil, err
	}
	if err := r.loadLines(ctx, bt); err != nil {
		return nil, err
	}
	return bt, nil
}

func (r *BankTransactionRepository) loadLines(ctx context.Context, bt *models.BankTransaction) error {
	rows, err := r.pool.Query(ctx,
		`SELECT line_item_id, COALESCE(description,''), quantity, unit_amount,
			COALESCE(account_code,''), COALESCE(tax_type,''), tax_amount, line_amount
		 FROM bank_transaction_line_items
		 WHERE bank_transaction_id=$1 ORDER BY line_item_id`,
		bt.BankTransactionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var li models.LineItem
		if err := rows.Scan(&li.LineItemID, &li.Description, &li.Quantity, &li.UnitAmount,
			&li.AccountCode, &li.TaxType, &li.TaxAmount, &li.LineAmount); err != nil {
			return err
		}
		bt.LineItems = append(bt.LineItems, li)
	}
	return rows.Err()
}

func (r *BankTransactionRepository) Create(ctx context.Context, orgID uuid.UUID, bt *models.BankTransaction) error {
	recalculateBankTx(bt)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `INSERT INTO bank_transactions (
		organisation_id, contact_id, bank_account_id, type,
		is_reconciled, date, reference, currency_code, currency_rate, url,
		status, line_amount_types, sub_total, total_tax, total
	) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,NULLIF($10,''),
		$11,$12,$13,$14,$15)
	  RETURNING bank_transaction_id, updated_date_utc`,
		orgID, bt.ContactID, bt.BankAccountID, bt.Type,
		bt.IsReconciled, bt.Date, bt.Reference, bt.CurrencyCode, bt.CurrencyRate, bt.URL,
		bt.Status, bt.LineAmountTypes, bt.SubTotal, bt.TotalTax, bt.Total,
	).Scan(&bt.BankTransactionID, &bt.UpdatedDateUTC); err != nil {
		return err
	}
	for _, li := range bt.LineItems {
		if _, err := tx.Exec(ctx,
			`INSERT INTO bank_transaction_line_items (
				bank_transaction_id, description, quantity, unit_amount,
				account_code, tax_type, tax_amount, line_amount)
			 VALUES ($1, NULLIF($2,''), $3,$4, NULLIF($5,''), NULLIF($6,''), $7,$8)`,
			bt.BankTransactionID, li.Description, li.Quantity, li.UnitAmount,
			li.AccountCode, li.TaxType, li.TaxAmount, li.LineAmount); err != nil {
			return err
		}
	}
	if bt.Status != "DELETED" {
		if err := postBankTransactionJournal(ctx, tx, orgID, bt); err != nil {
			return fmt.Errorf("post gl journal: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (r *BankTransactionRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	cmd, err := tx.Exec(ctx,
		`UPDATE bank_transactions SET status='DELETED', updated_date_utc=now()
		 WHERE organisation_id=$1 AND bank_transaction_id=$2 AND status <> 'DELETED'`,
		orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM gl_journals WHERE organisation_id=$1 AND source_type='BANKTRANSACTION' AND source_id=$2`,
		orgID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func recalculateBankTx(bt *models.BankTransaction) {
	sub := decimal.Zero
	tax := decimal.Zero
	for i := range bt.LineItems {
		li := &bt.LineItems[i]
		if li.Quantity.IsZero() {
			li.Quantity = decimal.NewFromInt(1)
		}
		li.LineAmount = li.Quantity.Mul(li.UnitAmount)
		sub = sub.Add(li.LineAmount)
		tax = tax.Add(li.TaxAmount)
	}
	bt.SubTotal = sub
	bt.TotalTax = tax
	switch bt.LineAmountTypes {
	case models.LineAmountTypesInclusive:
		bt.Total = sub
	case models.LineAmountTypesNoTax:
		bt.Total = sub
		bt.TotalTax = decimal.Zero
	default:
		bt.Total = sub.Add(tax)
	}
}
