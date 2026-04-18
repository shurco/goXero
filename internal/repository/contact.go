package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shurco/goxero/internal/models"
)

type ContactRepository struct {
	pool *pgxpool.Pool
}

const contactColumns = `
	contact_id, COALESCE(contact_number,''), COALESCE(account_number,''), contact_status,
	name, COALESCE(first_name,''), COALESCE(last_name,''), COALESCE(company_number,''),
	COALESCE(email_address,''), COALESCE(skype_user_name,''), COALESCE(bank_account_details,''),
	COALESCE(tax_number,''), COALESCE(accounts_receivable_tax_type,''),
	COALESCE(accounts_payable_tax_type,''), is_supplier, is_customer,
	COALESCE(default_currency,''), COALESCE(website,''), has_attachments, updated_date_utc`

func scanContact(row pgx.Row) (*models.Contact, error) {
	c := &models.Contact{}
	err := row.Scan(
		&c.ContactID, &c.ContactNumber, &c.AccountNumber, &c.ContactStatus,
		&c.Name, &c.FirstName, &c.LastName, &c.CompanyNumber,
		&c.EmailAddress, &c.SkypeUserName, &c.BankAccountDetails,
		&c.TaxNumber, &c.AccountsReceivableTaxType,
		&c.AccountsPayableTaxType, &c.IsSupplier, &c.IsCustomer,
		&c.DefaultCurrency, &c.Website, &c.HasAttachments, &c.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

type ContactFilter struct {
	Status     string
	Search     string
	IsCustomer *bool
	IsSupplier *bool
}

func (r *ContactRepository) List(ctx context.Context, orgID uuid.UUID, f ContactFilter, p models.Pagination) ([]models.Contact, int, error) {
	var sb strings.Builder
	sb.WriteString(" FROM contacts WHERE organisation_id=$1")
	args := []any{orgID}

	if f.Status != "" {
		args = append(args, f.Status)
		sb.WriteString(" AND contact_status=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		sb.WriteString(" AND (name ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(" OR email_address ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(")")
	}
	if f.IsCustomer != nil {
		args = append(args, *f.IsCustomer)
		sb.WriteString(" AND is_customer=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.IsSupplier != nil {
		args = append(args, *f.IsSupplier)
		sb.WriteString(" AND is_supplier=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}

	where := sb.String()

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*)"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, p.PageSize, p.Offset())
	query := "SELECT " + contactColumns + where + " ORDER BY name LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []models.Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, *c)
	}
	return list, total, rows.Err()
}

func (r *ContactRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.Contact, error) {
	q := "SELECT " + contactColumns + " FROM contacts WHERE organisation_id=$1 AND contact_id=$2"
	c, err := scanContact(r.pool.QueryRow(ctx, q, orgID, id))
	if err != nil {
		return nil, err
	}
	if err := r.loadAddresses(ctx, c); err != nil {
		return nil, err
	}
	if err := r.loadPhones(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (r *ContactRepository) loadAddresses(ctx context.Context, c *models.Contact) error {
	rows, err := r.pool.Query(ctx,
		`SELECT id, address_type, COALESCE(address_line1,''), COALESCE(address_line2,''),
			COALESCE(address_line3,''), COALESCE(address_line4,''),
			COALESCE(city,''), COALESCE(region,''), COALESCE(postal_code,''),
			COALESCE(country,''), COALESCE(attention_to,'')
		 FROM contact_addresses WHERE contact_id=$1 ORDER BY id`, c.ContactID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a models.Address
		if err := rows.Scan(&a.ID, &a.AddressType,
			&a.AddressLine1, &a.AddressLine2, &a.AddressLine3, &a.AddressLine4,
			&a.City, &a.Region, &a.PostalCode, &a.Country, &a.AttentionTo); err != nil {
			return err
		}
		c.Addresses = append(c.Addresses, a)
	}
	return rows.Err()
}

func (r *ContactRepository) loadPhones(ctx context.Context, c *models.Contact) error {
	rows, err := r.pool.Query(ctx,
		`SELECT id, phone_type, COALESCE(phone_number,''), COALESCE(phone_area_code,''), COALESCE(phone_country_code,'')
		 FROM contact_phones WHERE contact_id=$1 ORDER BY id`, c.ContactID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p models.Phone
		if err := rows.Scan(&p.ID, &p.PhoneType, &p.PhoneNumber, &p.PhoneAreaCode, &p.PhoneCountryCode); err != nil {
			return err
		}
		c.Phones = append(c.Phones, p)
	}
	return rows.Err()
}

func (r *ContactRepository) Create(ctx context.Context, orgID uuid.UUID, c *models.Contact) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	q := `INSERT INTO contacts (
		organisation_id, contact_number, account_number, contact_status,
		name, first_name, last_name, company_number,
		email_address, skype_user_name, bank_account_details,
		tax_number, accounts_receivable_tax_type, accounts_payable_tax_type,
		is_supplier, is_customer, default_currency, website
	) VALUES ($1,NULLIF($2,''),NULLIF($3,''), COALESCE(NULLIF($4,''),'ACTIVE'),
		$5, NULLIF($6,''), NULLIF($7,''), NULLIF($8,''),
		NULLIF($9,''), NULLIF($10,''), NULLIF($11,''),
		NULLIF($12,''), NULLIF($13,''), NULLIF($14,''),
		$15, $16, NULLIF($17,''), NULLIF($18,''))
	RETURNING contact_id, updated_date_utc`
	if err := tx.QueryRow(ctx, q,
		orgID, c.ContactNumber, c.AccountNumber, c.ContactStatus,
		c.Name, c.FirstName, c.LastName, c.CompanyNumber,
		c.EmailAddress, c.SkypeUserName, c.BankAccountDetails,
		c.TaxNumber, c.AccountsReceivableTaxType, c.AccountsPayableTaxType,
		c.IsSupplier, c.IsCustomer, c.DefaultCurrency, c.Website,
	).Scan(&c.ContactID, &c.UpdatedDateUTC); err != nil {
		return err
	}
	if err := replaceContactAddresses(ctx, tx, c.ContactID, c.Addresses); err != nil {
		return err
	}
	if err := replaceContactPhones(ctx, tx, c.ContactID, c.Phones); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ContactRepository) Update(ctx context.Context, orgID uuid.UUID, c *models.Contact) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	q := `UPDATE contacts SET
		contact_number=NULLIF($3,''), account_number=NULLIF($4,''), contact_status=$5,
		name=$6, first_name=NULLIF($7,''), last_name=NULLIF($8,''), company_number=NULLIF($9,''),
		email_address=NULLIF($10,''), skype_user_name=NULLIF($11,''), bank_account_details=NULLIF($12,''),
		tax_number=NULLIF($13,''), accounts_receivable_tax_type=NULLIF($14,''), accounts_payable_tax_type=NULLIF($15,''),
		is_supplier=$16, is_customer=$17, default_currency=NULLIF($18,''), website=NULLIF($19,''),
		updated_date_utc=now()
		WHERE organisation_id=$1 AND contact_id=$2
		RETURNING updated_date_utc`
	if err := tx.QueryRow(ctx, q,
		orgID, c.ContactID,
		c.ContactNumber, c.AccountNumber, c.ContactStatus,
		c.Name, c.FirstName, c.LastName, c.CompanyNumber,
		c.EmailAddress, c.SkypeUserName, c.BankAccountDetails,
		c.TaxNumber, c.AccountsReceivableTaxType, c.AccountsPayableTaxType,
		c.IsSupplier, c.IsCustomer, c.DefaultCurrency, c.Website,
	).Scan(&c.UpdatedDateUTC); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if c.Addresses != nil {
		if err := replaceContactAddresses(ctx, tx, c.ContactID, c.Addresses); err != nil {
			return err
		}
	}
	if c.Phones != nil {
		if err := replaceContactPhones(ctx, tx, c.ContactID, c.Phones); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func replaceContactAddresses(ctx context.Context, tx pgx.Tx, contactID uuid.UUID, addrs []models.Address) error {
	if _, err := tx.Exec(ctx, `DELETE FROM contact_addresses WHERE contact_id=$1`, contactID); err != nil {
		return err
	}
	for _, a := range addrs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO contact_addresses
				(contact_id, address_type, address_line1, address_line2, address_line3, address_line4,
				 city, region, postal_code, country, attention_to)
			 VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),
			         NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),NULLIF($11,''))`,
			contactID, a.AddressType, a.AddressLine1, a.AddressLine2, a.AddressLine3, a.AddressLine4,
			a.City, a.Region, a.PostalCode, a.Country, a.AttentionTo); err != nil {
			return err
		}
	}
	return nil
}

func replaceContactPhones(ctx context.Context, tx pgx.Tx, contactID uuid.UUID, phones []models.Phone) error {
	if _, err := tx.Exec(ctx, `DELETE FROM contact_phones WHERE contact_id=$1`, contactID); err != nil {
		return err
	}
	for _, p := range phones {
		if _, err := tx.Exec(ctx,
			`INSERT INTO contact_phones
				(contact_id, phone_type, phone_number, phone_area_code, phone_country_code)
			 VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''))`,
			contactID, p.PhoneType, p.PhoneNumber, p.PhoneAreaCode, p.PhoneCountryCode); err != nil {
			return err
		}
	}
	return nil
}

func (r *ContactRepository) Archive(ctx context.Context, orgID, id uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE contacts SET contact_status='ARCHIVED', updated_date_utc=now()
		 WHERE organisation_id=$1 AND contact_id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
