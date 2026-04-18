package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

type TrackingCategoryRepository struct {
	pool *pgxpool.Pool
}

func (r *TrackingCategoryRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.TrackingCategory, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT tracking_category_id, name, status
		   FROM tracking_categories
		  WHERE organisation_id=$1
		  ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.TrackingCategory
	ids := []uuid.UUID{}
	for rows.Next() {
		var t models.TrackingCategory
		if err := rows.Scan(&t.TrackingCategoryID, &t.Name, &t.Status); err != nil {
			return nil, err
		}
		out = append(out, t)
		ids = append(ids, t.TrackingCategoryID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}
	opts, err := r.loadOptions(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Options = opts[out[i].TrackingCategoryID]
	}
	return out, nil
}

func (r *TrackingCategoryRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.TrackingCategory, error) {
	var t models.TrackingCategory
	err := r.pool.QueryRow(ctx,
		`SELECT tracking_category_id, name, status
		   FROM tracking_categories
		  WHERE organisation_id=$1 AND tracking_category_id=$2`,
		orgID, id).Scan(&t.TrackingCategoryID, &t.Name, &t.Status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	opts, err := r.loadOptions(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	t.Options = opts[id]
	return &t, nil
}

func (r *TrackingCategoryRepository) loadOptions(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]models.TrackingOption, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT tracking_category_id, tracking_option_id, name, status
		   FROM tracking_options
		  WHERE tracking_category_id = ANY($1) ORDER BY name`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]models.TrackingOption)
	for rows.Next() {
		var cid uuid.UUID
		var opt models.TrackingOption
		if err := rows.Scan(&cid, &opt.TrackingOptionID, &opt.Name, &opt.Status); err != nil {
			return nil, err
		}
		out[cid] = append(out[cid], opt)
	}
	return out, rows.Err()
}

func (r *TrackingCategoryRepository) Create(ctx context.Context, orgID uuid.UUID, t *models.TrackingCategory) error {
	if t.Status == "" {
		t.Status = "ACTIVE"
	}
	if err := r.pool.QueryRow(ctx,
		`INSERT INTO tracking_categories (organisation_id, name, status)
		 VALUES ($1,$2,$3) RETURNING tracking_category_id`,
		orgID, t.Name, t.Status).Scan(&t.TrackingCategoryID); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (r *TrackingCategoryRepository) Update(ctx context.Context, orgID uuid.UUID, t *models.TrackingCategory) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE tracking_categories SET name=$3, status=COALESCE(NULLIF($4,''), status)
		  WHERE organisation_id=$1 AND tracking_category_id=$2`,
		orgID, t.TrackingCategoryID, t.Name, t.Status)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *TrackingCategoryRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE tracking_categories SET status='DELETED'
		  WHERE organisation_id=$1 AND tracking_category_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *TrackingCategoryRepository) AddOption(ctx context.Context, orgID, categoryID uuid.UUID, opt *models.TrackingOption) error {
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM tracking_categories
			 WHERE organisation_id=$1 AND tracking_category_id=$2)`,
		orgID, categoryID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if opt.Status == "" {
		opt.Status = "ACTIVE"
	}
	if err := r.pool.QueryRow(ctx,
		`INSERT INTO tracking_options (tracking_category_id, name, status)
		 VALUES ($1,$2,$3) RETURNING tracking_option_id`,
		categoryID, opt.Name, opt.Status).Scan(&opt.TrackingOptionID); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}
