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

type OrganisationRepository struct {
	pool *pgxpool.Pool
}

const organisationColumns = `
	organisation_id, api_key, name,
	COALESCE(legal_name,''), COALESCE(short_code,''),
	COALESCE(organisation_type,''), COALESCE(country_code,''),
	base_currency, COALESCE(timezone,''),
	financial_year_end_day, financial_year_end_month,
	COALESCE(tax_number,''), COALESCE(line_of_business,''), COALESCE(registration_number,''),
	COALESCE(description,''), COALESCE(profile, '{}'::jsonb),
	is_demo_company, organisation_status, created_at, updated_at`

// organisationColumnsO is the same projection with alias `o` for JOIN queries.
const organisationColumnsO = `
	o.organisation_id, o.api_key, o.name,
	COALESCE(o.legal_name,''), COALESCE(o.short_code,''),
	COALESCE(o.organisation_type,''), COALESCE(o.country_code,''),
	o.base_currency, COALESCE(o.timezone,''),
	o.financial_year_end_day, o.financial_year_end_month,
	COALESCE(o.tax_number,''), COALESCE(o.line_of_business,''), COALESCE(o.registration_number,''),
	COALESCE(o.description,''), COALESCE(o.profile, '{}'::jsonb),
	o.is_demo_company, o.organisation_status, o.created_at, o.updated_at`

func scanOrganisation(row pgx.Row) (*models.Organisation, error) {
	o := &models.Organisation{}
	var profileBytes []byte
	err := row.Scan(
		&o.OrganisationID, &o.APIKey, &o.Name, &o.LegalName, &o.ShortCode,
		&o.OrganisationType, &o.CountryCode, &o.BaseCurrency, &o.Timezone,
		&o.FinancialYearEndDay, &o.FinancialYearEndMonth, &o.TaxNumber,
		&o.LineOfBusiness, &o.RegistrationNumber,
		&o.Description, &profileBytes,
		&o.IsDemoCompany,
		&o.OrganisationStatus, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(profileBytes) > 0 && string(profileBytes) != "null" {
		_ = json.Unmarshal(profileBytes, &o.Profile)
	}
	return o, nil
}

func (r *OrganisationRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Organisation, error) {
	q := "SELECT " + organisationColumns + " FROM organisations WHERE organisation_id=$1"
	return scanOrganisation(r.pool.QueryRow(ctx, q, id))
}

// ListForUser returns every organisation the given user is a member of using a
// single JOIN (avoids the previous N+1 fan-out from organisation_users).
func (r *OrganisationRepository) ListForUser(ctx context.Context, userID uuid.UUID) ([]models.Organisation, error) {
	q := `SELECT ` + organisationColumnsO + `
		FROM organisations o
		JOIN organisation_users ou ON ou.organisation_id = o.organisation_id
		WHERE ou.user_id = $1
		ORDER BY o.name`
	return r.queryMany(ctx, q, userID)
}

func (r *OrganisationRepository) queryMany(ctx context.Context, q string, args ...any) ([]models.Organisation, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Organisation
	for rows.Next() {
		o, err := scanOrganisation(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *o)
	}
	return list, rows.Err()
}

func (r *OrganisationRepository) Create(ctx context.Context, o *models.Organisation) error {
	profileJSON, err := json.Marshal(o.Profile)
	if err != nil {
		return err
	}
	fyDay := o.FinancialYearEndDay
	if fyDay < 1 || fyDay > 31 {
		fyDay = 31
	}
	fyMonth := o.FinancialYearEndMonth
	if fyMonth < 1 || fyMonth > 12 {
		fyMonth = 12
	}
	o.FinancialYearEndDay = fyDay
	o.FinancialYearEndMonth = fyMonth

	q := `INSERT INTO organisations (
		name, legal_name, short_code, organisation_type, country_code,
		base_currency, timezone, tax_number, line_of_business, registration_number,
		financial_year_end_day, financial_year_end_month,
		description, profile
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb)
	RETURNING organisation_id, api_key, created_at, updated_at`
	return r.pool.QueryRow(ctx, q,
		o.Name, o.LegalName, o.ShortCode, o.OrganisationType, o.CountryCode,
		o.BaseCurrency, o.Timezone, o.TaxNumber, o.LineOfBusiness, o.RegistrationNumber,
		fyDay, fyMonth,
		o.Description,
		profileJSON,
	).Scan(&o.OrganisationID, &o.APIKey, &o.CreatedAt, &o.UpdatedAt)
}

// Update persists editable organisation fields for the tenant.
func (r *OrganisationRepository) Update(ctx context.Context, o *models.Organisation) error {
	profileJSON, err := json.Marshal(o.Profile)
	if err != nil {
		return err
	}
	q := `UPDATE organisations SET
		name = $2,
		legal_name = NULLIF($3,''),
		organisation_type = NULLIF($4,''),
		country_code = NULLIF(UPPER($5),''),
		timezone = NULLIF($6,''),
		tax_number = NULLIF($7,''),
		line_of_business = NULLIF($8,''),
		registration_number = NULLIF($9,''),
		description = NULLIF($10,''),
		profile = $11::jsonb,
		updated_at = now()
	WHERE organisation_id = $1`
	ct, err := r.pool.Exec(ctx, q,
		o.OrganisationID,
		o.Name,
		o.LegalName,
		o.OrganisationType,
		o.CountryCode,
		o.Timezone,
		o.TaxNumber,
		o.LineOfBusiness,
		o.RegistrationNumber,
		o.Description,
		profileJSON,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
