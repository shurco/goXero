package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

// conversionBalanceSourceType keys the one journal an organisation's conversion
// balances live in. The source id is the organisation itself: an organisation
// converts once, so the screen owns exactly one journal and a save replaces it
// rather than writing a second set of opening balances beside the first.
const conversionBalanceSourceType = "CONVERSIONBALANCE"

// conversionBalanceReference is the narration on that journal. It is the words
// the reference organisation's own journal 388 carries.
const conversionBalanceReference = "Conversion Balance"

type ConversionBalanceRepository struct {
	pool *pgxpool.Pool
}

// Get returns the organisation's conversion balances. An organisation that has
// not converted reads back as an empty balance -- no date, no lines -- rather
// than as an error: "no opening balances yet" is a state of the screen, not a
// missing record.
func (r *ConversionBalanceRepository) Get(ctx context.Context, orgID uuid.UUID) (*models.ConversionBalance, error) {
	cb := &models.ConversionBalance{Lines: []models.ConversionBalanceLine{}}
	var date *time.Time
	if err := r.pool.QueryRow(ctx,
		`SELECT conversion_date, conversion_balances_locked
		   FROM organisations WHERE organisation_id=$1`, orgID,
	).Scan(&date, &cb.Locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if date != nil {
		cb.ConversionDate = date.Format(time.DateOnly)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT a.account_id, COALESCE(a.code,''), l.net_amount
		   FROM gl_journals j
		   JOIN gl_journal_lines l ON l.journal_id = j.journal_id
		   JOIN accounts a ON a.account_id = l.account_id
		  WHERE j.organisation_id=$1 AND j.source_type=$2 AND j.source_id=$1
		  ORDER BY a.code`, orgID, conversionBalanceSourceType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var line models.ConversionBalanceLine
		if err := rows.Scan(&line.AccountID, &line.Code, &line.Amount); err != nil {
			return nil, err
		}
		cb.Lines = append(cb.Lines, line)
	}
	return cb, rows.Err()
}

// Save replaces the organisation's conversion balances, the date they are
// stated as at and the lock, in one transaction.
//
// The lines are posted as the organisation's conversion journal, so the screen
// and the ledger cannot disagree: whatever the Trial Balance prints is what the
// screen stored. The rows need not balance on their own -- the difference goes
// to the chart's Historical Adjustment account, which is what the screen's
// "Adjustments" figure is -- and an unbalanced set is refused when the chart
// has no such account to carry the difference, rather than posted unbalanced.
func (r *ConversionBalanceRepository) Save(ctx context.Context, orgID uuid.UUID, cb *models.ConversionBalance) error {
	date, err := time.Parse(time.DateOnly, cb.ConversionDate)
	if err != nil {
		return fmt.Errorf("ConversionDate must be YYYY-MM-DD: %w", ErrInvalidInput)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	lines, err := conversionBalanceLines(ctx, tx, orgID, cb.Lines)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE organisations
		    SET conversion_date=$2, conversion_balances_locked=$3, updated_at=now()
		  WHERE organisation_id=$1`, orgID, date, cb.Locked); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM gl_journals
		  WHERE organisation_id=$1 AND source_type=$2 AND source_id=$1`,
		orgID, conversionBalanceSourceType); err != nil {
		return err
	}
	if err := insertJournal(ctx, tx, orgID, conversionBalanceSourceType, orgID,
		date, conversionBalanceReference, lines); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// conversionBalanceLines turns the entered rows into ledger lines, offsetting a
// difference between debits and credits against the Historical Adjustment
// account the way Xero does. Rows with no account or a zero amount are dropped:
// they post nothing, and a zero line would only make the journal claim an entry
// the person never made.
func conversionBalanceLines(
	ctx context.Context, tx pgx.Tx, orgID uuid.UUID, rows []models.ConversionBalanceLine,
) ([]journalLineInput, error) {
	lines := make([]journalLineInput, 0, len(rows)+1)
	sum := decimal.Zero
	seen := make(map[uuid.UUID]bool, len(rows))
	for _, row := range rows {
		if row.AccountID == uuid.Nil || row.Amount.IsZero() {
			continue
		}
		if seen[row.AccountID] {
			return nil, fmt.Errorf("account %s is entered twice: %w", row.AccountID, ErrInvalidInput)
		}
		seen[row.AccountID] = true
		if err := requireAccountInOrg(ctx, tx, orgID, row.AccountID); err != nil {
			return nil, err
		}
		lines = append(lines, journalLineInput{
			AccountID:   row.AccountID,
			Description: conversionBalanceReference,
			NetAmount:   row.Amount,
		})
		sum = sum.Add(row.Amount)
	}
	if len(lines) == 0 || sum.IsZero() {
		return lines, nil
	}
	adjustment, ok, err := systemAccountIDOrNil(ctx, tx, orgID, models.SystemAccountHistorical)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf(
			"debits and credits differ by %s and the chart has no Historical Adjustment account to carry the difference: %w",
			sum.String(), ErrInvalidInput)
	}
	return append(lines, journalLineInput{
		AccountID:   adjustment,
		Description: conversionBalanceReference,
		NetAmount:   sum.Neg(),
	}), nil
}
