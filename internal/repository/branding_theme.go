package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

type BrandingThemeRepository struct {
	pool *pgxpool.Pool
}

func (r *BrandingThemeRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.BrandingTheme, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT branding_theme_id, name, sort_order, COALESCE(logo_url,''), created_at
		   FROM branding_themes
		  WHERE organisation_id=$1
		  ORDER BY sort_order, name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BrandingTheme
	for rows.Next() {
		var b models.BrandingTheme
		if err := rows.Scan(&b.BrandingThemeID, &b.Name, &b.SortOrder, &b.LogoURL, &b.CreatedDateUTC); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *BrandingThemeRepository) Create(ctx context.Context, orgID uuid.UUID, b *models.BrandingTheme) error {
	if err := r.pool.QueryRow(ctx,
		`INSERT INTO branding_themes (organisation_id, name, sort_order, logo_url)
		 VALUES ($1,$2,$3,NULLIF($4,''))
		 RETURNING branding_theme_id, created_at`,
		orgID, b.Name, b.SortOrder, b.LogoURL).Scan(&b.BrandingThemeID, &b.CreatedDateUTC); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}
