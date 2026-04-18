package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

type BankTransferRepository struct {
	pool *pgxpool.Pool
}

func (r *BankTransferRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.BankTransfer, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT bank_transfer_id, from_bank_account_id, to_bank_account_id,
		        amount, date, COALESCE(reference,''), currency_rate, has_attachments, created_at
		   FROM bank_transfers
		  WHERE organisation_id=$1 ORDER BY date DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BankTransfer
	for rows.Next() {
		var t models.BankTransfer
		if err := rows.Scan(
			&t.BankTransferID, &t.FromBankAccountID, &t.ToBankAccountID,
			&t.Amount, &t.Date, &t.Reference, &t.CurrencyRate, &t.HasAttachments, &t.CreatedDateUTC,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *BankTransferRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.BankTransfer, error) {
	var t models.BankTransfer
	err := r.pool.QueryRow(ctx,
		`SELECT bank_transfer_id, from_bank_account_id, to_bank_account_id,
		        amount, date, COALESCE(reference,''), currency_rate, has_attachments, created_at
		   FROM bank_transfers WHERE organisation_id=$1 AND bank_transfer_id=$2`,
		orgID, id).Scan(
		&t.BankTransferID, &t.FromBankAccountID, &t.ToBankAccountID,
		&t.Amount, &t.Date, &t.Reference, &t.CurrencyRate, &t.HasAttachments, &t.CreatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (r *BankTransferRepository) Create(ctx context.Context, orgID uuid.UUID, t *models.BankTransfer) error {
	if !t.Amount.IsPositive() {
		return fmt.Errorf("amount must be positive")
	}
	if t.FromBankAccountID == t.ToBankAccountID {
		return fmt.Errorf("cannot transfer to same account")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx,
		`INSERT INTO bank_transfers (organisation_id, from_bank_account_id, to_bank_account_id,
			amount, date, reference, currency_rate, has_attachments)
		 VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8)
		 RETURNING bank_transfer_id, created_at`,
		orgID, t.FromBankAccountID, t.ToBankAccountID, t.Amount, t.Date, t.Reference,
		t.CurrencyRate, t.HasAttachments).Scan(&t.BankTransferID, &t.CreatedDateUTC); err != nil {
		return err
	}
	if err := postBankTransferJournal(ctx, tx, orgID, t); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
