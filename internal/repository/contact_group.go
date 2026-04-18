package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

type ContactGroupRepository struct {
	pool *pgxpool.Pool
}

func (r *ContactGroupRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.ContactGroup, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT contact_group_id, name, status
		   FROM contact_groups
		  WHERE organisation_id=$1
		  ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []models.ContactGroup
	for rows.Next() {
		var g models.ContactGroup
		if err := rows.Scan(&g.ContactGroupID, &g.Name, &g.Status); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (r *ContactGroupRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.ContactGroup, error) {
	var g models.ContactGroup
	err := r.pool.QueryRow(ctx,
		`SELECT contact_group_id, name, status
		   FROM contact_groups
		  WHERE organisation_id=$1 AND contact_group_id=$2`, orgID, id).
		Scan(&g.ContactGroupID, &g.Name, &g.Status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT c.contact_id, c.name
		   FROM contact_group_members m
		   JOIN contacts c ON c.contact_id = m.contact_id
		  WHERE m.contact_group_id=$1
		  ORDER BY c.name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c models.Contact
		if err := rows.Scan(&c.ContactID, &c.Name); err != nil {
			return nil, err
		}
		g.Contacts = append(g.Contacts, c)
	}
	return &g, rows.Err()
}

func (r *ContactGroupRepository) Create(ctx context.Context, orgID uuid.UUID, g *models.ContactGroup) error {
	if g.Status == "" {
		g.Status = "ACTIVE"
	}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO contact_groups (organisation_id, name, status)
		 VALUES ($1, $2, $3)
		 RETURNING contact_group_id`,
		orgID, g.Name, g.Status).Scan(&g.ContactGroupID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (r *ContactGroupRepository) Update(ctx context.Context, orgID uuid.UUID, g *models.ContactGroup) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE contact_groups
		    SET name=$3, status=COALESCE(NULLIF($4,''), status)
		  WHERE organisation_id=$1 AND contact_group_id=$2`,
		orgID, g.ContactGroupID, g.Name, g.Status)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ContactGroupRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE contact_groups SET status='DELETED'
		 WHERE organisation_id=$1 AND contact_group_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddContacts adds contacts to a group. Duplicates are silently ignored.
func (r *ContactGroupRepository) AddContacts(ctx context.Context, orgID, groupID uuid.UUID, contactIDs []uuid.UUID) error {
	if len(contactIDs) == 0 {
		return nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM contact_groups
			 WHERE organisation_id=$1 AND contact_group_id=$2)`,
		orgID, groupID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	for _, cid := range contactIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO contact_group_members (contact_group_id, contact_id)
			 VALUES ($1, $2) ON CONFLICT DO NOTHING`, groupID, cid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *ContactGroupRepository) RemoveContact(ctx context.Context, orgID, groupID, contactID uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`DELETE FROM contact_group_members
		  WHERE contact_group_id=$1 AND contact_id=$2
		    AND contact_group_id IN (
		        SELECT contact_group_id FROM contact_groups WHERE organisation_id=$3)`,
		groupID, contactID, orgID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
