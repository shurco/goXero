package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

type BankRuleRepository struct {
	pool *pgxpool.Pool
}

func (r *BankRuleRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.BankRule, error) {
	// Ordered the way the rules are evaluated: first match wins, so sort_order
	// is significant and name is only a tie-break.
	q := `SELECT bank_rule_id, rule_type, name, COALESCE(definition,'{}'::jsonb), is_active, sort_order, created_at, updated_at
		FROM bank_rules WHERE organisation_id=$1 ORDER BY sort_order ASC, name ASC`
	rows, err := r.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BankRule
	for rows.Next() {
		br, err := scanBankRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *br)
	}
	return out, rows.Err()
}

func scanBankRule(row pgx.Row) (*models.BankRule, error) {
	br := &models.BankRule{}
	var defBytes []byte
	err := row.Scan(&br.BankRuleID, &br.RuleType, &br.Name, &defBytes, &br.IsActive, &br.SortOrder, &br.CreatedAt, &br.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(defBytes) > 0 && string(defBytes) != "null" {
		if err := json.Unmarshal(defBytes, &br.Definition); err != nil {
			return nil, err
		}
	}
	return br, nil
}

func (r *BankRuleRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.BankRule, error) {
	q := `SELECT bank_rule_id, rule_type, name, COALESCE(definition,'{}'::jsonb), is_active, sort_order, created_at, updated_at
		FROM bank_rules WHERE organisation_id=$1 AND bank_rule_id=$2`
	return scanBankRule(r.pool.QueryRow(ctx, q, orgID, id))
}

func (r *BankRuleRepository) Create(ctx context.Context, orgID uuid.UUID, br *models.BankRule) error {
	defJSON, err := json.Marshal(br.Definition)
	if err != nil {
		return err
	}
	// Xero appends a new rule last. Without an explicit order it would default
	// to 0 and jump ahead of every rule that Reorder has already numbered.
	q := `INSERT INTO bank_rules (organisation_id, rule_type, name, definition, is_active, sort_order)
		VALUES ($1,$2,$3,$4::jsonb,$5,
			COALESCE((SELECT MAX(sort_order)+1 FROM bank_rules WHERE organisation_id=$1), 0))
		RETURNING bank_rule_id, sort_order, created_at, updated_at`
	return r.pool.QueryRow(ctx, q, orgID, br.RuleType, br.Name, defJSON, br.IsActive).Scan(
		&br.BankRuleID, &br.SortOrder, &br.CreatedAt, &br.UpdatedAt,
	)
}

func (r *BankRuleRepository) Update(ctx context.Context, orgID uuid.UUID, br *models.BankRule) error {
	defJSON, err := json.Marshal(br.Definition)
	if err != nil {
		return err
	}
	q := `UPDATE bank_rules SET rule_type=$3, name=$4, definition=$5::jsonb, is_active=$6, updated_at=now()
		WHERE organisation_id=$1 AND bank_rule_id=$2`
	ct, err := r.pool.Exec(ctx, q, orgID, br.BankRuleID, br.RuleType, br.Name, defJSON, br.IsActive)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *BankRuleRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM bank_rules WHERE organisation_id=$1 AND bank_rule_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Reorder rewrites the evaluation order of a tenant's rules. The whole list is
// sent, so a concurrent edit cannot silently interleave two orderings — and any
// id the tenant does not own simply matches no row.
func (r *BankRuleRepository) Reorder(ctx context.Context, orgID uuid.UUID, orderedIDs []uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, id := range orderedIDs {
		if _, err := tx.Exec(ctx,
			`UPDATE bank_rules SET sort_order=$3, updated_at=now()
			 WHERE organisation_id=$1 AND bank_rule_id=$2`,
			orgID, id, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
