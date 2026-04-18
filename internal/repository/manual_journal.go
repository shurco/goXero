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

type ManualJournalRepository struct {
	pool *pgxpool.Pool
}

// List returns manual journals ordered by date descending. The endpoint-wide
// pagination is respected.
func (r *ManualJournalRepository) List(ctx context.Context, orgID uuid.UUID, p models.Pagination) ([]models.ManualJournal, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM manual_journals WHERE organisation_id=$1`, orgID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT manual_journal_id, narration, date, line_amount_types, status,
		        COALESCE(url,''), show_on_cash_basis_reports, updated_date_utc
		   FROM manual_journals WHERE organisation_id=$1
		   ORDER BY date DESC, created_at DESC
		   LIMIT $2 OFFSET $3`, orgID, p.PageSize, p.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.ManualJournal
	for rows.Next() {
		var mj models.ManualJournal
		if err := rows.Scan(
			&mj.ManualJournalID, &mj.Narration, &mj.Date, &mj.LineAmountTypes, &mj.Status,
			&mj.URL, &mj.ShowOnCashBasisReports, &mj.UpdatedDateUTC,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, mj)
	}
	return out, total, rows.Err()
}

func (r *ManualJournalRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.ManualJournal, error) {
	var mj models.ManualJournal
	err := r.pool.QueryRow(ctx,
		`SELECT manual_journal_id, narration, date, line_amount_types, status,
		        COALESCE(url,''), show_on_cash_basis_reports, updated_date_utc
		   FROM manual_journals WHERE organisation_id=$1 AND manual_journal_id=$2`,
		orgID, id).Scan(
		&mj.ManualJournalID, &mj.Narration, &mj.Date, &mj.LineAmountTypes, &mj.Status,
		&mj.URL, &mj.ShowOnCashBasisReports, &mj.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT line_id, COALESCE(description,''), COALESCE(account_code,''),
		        COALESCE(tax_type,''), tax_amount, line_amount
		   FROM manual_journal_lines WHERE manual_journal_id=$1 ORDER BY line_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l models.ManualJournalLine
		if err := rows.Scan(&l.LineID, &l.Description, &l.AccountCode,
			&l.TaxType, &l.TaxAmount, &l.LineAmount); err != nil {
			return nil, err
		}
		mj.JournalLines = append(mj.JournalLines, l)
	}
	return &mj, rows.Err()
}

func (r *ManualJournalRepository) Create(ctx context.Context, orgID uuid.UUID, mj *models.ManualJournal) error {
	if err := validateManualJournalBalance(mj); err != nil {
		return err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx,
		`INSERT INTO manual_journals (
			organisation_id, narration, date, line_amount_types, status,
			url, show_on_cash_basis_reports)
		 VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7)
		 RETURNING manual_journal_id, updated_date_utc`,
		orgID, mj.Narration, mj.Date, mj.LineAmountTypes, mj.Status,
		mj.URL, mj.ShowOnCashBasisReports,
	).Scan(&mj.ManualJournalID, &mj.UpdatedDateUTC); err != nil {
		return err
	}
	for i := range mj.JournalLines {
		l := &mj.JournalLines[i]
		if err := tx.QueryRow(ctx,
			`INSERT INTO manual_journal_lines (
				manual_journal_id, description, account_code, tax_type, tax_amount, line_amount)
			 VALUES ($1, NULLIF($2,''), NULLIF($3,''), NULLIF($4,''), $5, $6)
			 RETURNING line_id`,
			mj.ManualJournalID, l.Description, l.AccountCode, l.TaxType, l.TaxAmount, l.LineAmount,
		).Scan(&l.LineID); err != nil {
			return err
		}
	}
	if mj.Status == models.InvoiceStatusAuthorised || mj.Status == "POSTED" {
		if err := postManualJournal(ctx, tx, orgID, mj); err != nil {
			return fmt.Errorf("post gl journal: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (r *ManualJournalRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	cmd, err := tx.Exec(ctx,
		`UPDATE manual_journals SET status='DELETED', updated_date_utc=now()
		  WHERE organisation_id=$1 AND manual_journal_id=$2 AND status <> 'DELETED'`,
		orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM gl_journals WHERE organisation_id=$1 AND source_type='MANUALJOURNAL' AND source_id=$2`,
		orgID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func validateManualJournalBalance(mj *models.ManualJournal) error {
	if len(mj.JournalLines) < 2 {
		return fmt.Errorf("manual journal needs at least two lines")
	}
	sum := decimal.Zero
	for _, l := range mj.JournalLines {
		if l.AccountCode == "" {
			return fmt.Errorf("line missing AccountCode")
		}
		sum = sum.Add(l.LineAmount)
	}
	if !sum.IsZero() {
		return fmt.Errorf("manual journal lines must sum to zero (got %s)", sum.String())
	}
	return nil
}
