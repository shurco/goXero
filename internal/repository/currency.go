package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

// CurrencyRepository backs the Currencies API.
// Xero treats currencies as org-scoped: a tenant can only transact in
// currencies that were `Added` to their organisation first.
type CurrencyRepository struct {
	pool *pgxpool.Pool
}

func (r *CurrencyRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.Currency, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT code, COALESCE(description,'') FROM currencies
		 WHERE organisation_id=$1 ORDER BY code`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Currency, 0)
	for rows.Next() {
		var c models.Currency
		if err := rows.Scan(&c.Code, &c.Description); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *CurrencyRepository) Create(ctx context.Context, orgID uuid.UUID, cur *models.Currency) error {
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO currencies (organisation_id, code, description)
		 VALUES ($1, $2, NULLIF($3,''))
		 ON CONFLICT DO NOTHING`,
		orgID, cur.Code, cur.Description); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}
