package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

type DBUser struct {
	UserID       uuid.UUID
	Email        string
	PasswordHash string
	FirstName    string
	LastName     string
	IsSubscriber bool
	CreatedAt    time.Time
}

// userSelect is the canonical column list shared by GetByEmail/GetByID/Create.
// Keeping it in one place avoids the Scan/SELECT lists drifting apart.
const userSelect = `user_id, email, password_hash,
	COALESCE(first_name,''), COALESCE(last_name,''),
	is_subscriber, created_at`

func scanUser(row pgx.Row) (*DBUser, error) {
	u := &DBUser{}
	err := row.Scan(&u.UserID, &u.Email, &u.PasswordHash, &u.FirstName, &u.LastName, &u.IsSubscriber, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*DBUser, error) {
	return scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userSelect+` FROM users WHERE email = $1`, email))
}

func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*DBUser, error) {
	return scanUser(r.pool.QueryRow(ctx,
		`SELECT `+userSelect+` FROM users WHERE user_id = $1`, id))
}

func (r *UserRepository) Create(ctx context.Context, email, passwordHash, firstName, lastName string) (*DBUser, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, first_name, last_name)
		 VALUES ($1,$2,$3,$4)
		 RETURNING `+userSelect,
		email, passwordHash, firstName, lastName,
	)
	u, err := scanUser(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return u, nil
}

func (r *UserRepository) LinkOrganisation(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO organisation_users (organisation_id, user_id, role)
		 VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
		orgID, userID, role)
	return err
}

// ListForOrganisation returns every user linked to the given organisation,
// surfaced as Xero `User` DTOs (Xero's GET /Users endpoint).
func (r *UserRepository) ListForOrganisation(ctx context.Context, orgID uuid.UUID) ([]models.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT u.user_id, u.email, COALESCE(u.first_name,''), COALESCE(u.last_name,''),
		        u.is_subscriber, ou.role, u.created_at
		   FROM users u
		   JOIN organisation_users ou ON ou.user_id = u.user_id
		  WHERE ou.organisation_id=$1
		  ORDER BY u.email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.UserID, &u.Email, &u.FirstName, &u.LastName,
			&u.IsSubscriber, &u.OrganisationRole, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// HasOrganisationAccess reports whether the user is a member of the organisation.
func (r *UserRepository) HasOrganisationAccess(ctx context.Context, userID, orgID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM organisation_users
			WHERE user_id=$1 AND organisation_id=$2
		)`, userID, orgID).Scan(&exists)
	return exists, err
}
