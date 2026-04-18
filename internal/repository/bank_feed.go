package repository

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

// BankFeedRepository is the data access layer behind the `/bank-feeds/*`
// endpoints — stores connections, their discovered accounts and the staging
// rows that flow into bank_transactions after reconciliation.
type BankFeedRepository struct {
	pool *pgxpool.Pool
}

// CreateConnection inserts a PENDING connection with whatever metadata we
// already have (provider + institution + auth URL). The caller will later
// update it with the external reference returned by the provider.
func (r *BankFeedRepository) CreateConnection(ctx context.Context, orgID uuid.UUID, c *models.BankFeedConnection) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO bank_feed_connections
			(organisation_id, provider, status, institution_id, institution_name,
			 external_reference, auth_url)
		 VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''))
		 RETURNING connection_id, created_at, updated_at`,
		orgID, c.Provider, c.Status, c.InstitutionID, c.InstitutionName,
		c.ExternalReference, c.AuthURL,
	).Scan(&c.ConnectionID, &c.CreatedAt, &c.UpdatedAt)
}

// UpdateConnectionStatus transitions a connection's status, optionally setting
// last_error / last_synced_at. Passing an empty string leaves the existing
// value untouched (NULLIF pattern).
func (r *BankFeedRepository) UpdateConnectionStatus(ctx context.Context, orgID, connID uuid.UUID, status, lastError string, syncedAt *time.Time) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET status         = $3,
		     last_error     = CASE WHEN $4 = '' THEN last_error ELSE $4 END,
		     last_synced_at = COALESCE($5, last_synced_at),
		     updated_at     = now()
		 WHERE organisation_id = $1 AND connection_id = $2`,
		orgID, connID, status, lastError, syncedAt)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListConnections returns every connection for a tenant, most-recent first.
// Accounts are loaded eagerly because the UI always displays them together.
func (r *BankFeedRepository) ListConnections(ctx context.Context, orgID uuid.UUID) ([]models.BankFeedConnection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT connection_id, provider, status,
		        COALESCE(institution_id,''), COALESCE(institution_name,''),
		        COALESCE(external_reference,''), COALESCE(auth_url,''),
		        COALESCE(last_error,''), last_synced_at,
		        created_at, updated_at
		 FROM bank_feed_connections
		 WHERE organisation_id = $1
		 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BankFeedConnection
	for rows.Next() {
		var c models.BankFeedConnection
		if err := rows.Scan(
			&c.ConnectionID, &c.Provider, &c.Status,
			&c.InstitutionID, &c.InstitutionName,
			&c.ExternalReference, &c.AuthURL,
			&c.LastError, &c.LastSyncedAt,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		accs, err := r.listAccountsByConnection(ctx, out[i].ConnectionID)
		if err != nil {
			return nil, err
		}
		out[i].Accounts = accs
	}
	return out, nil
}

// GetConnection is the one-row variant used by sync/finalize handlers that
// need the external reference + provider name.
func (r *BankFeedRepository) GetConnection(ctx context.Context, orgID, connID uuid.UUID) (*models.BankFeedConnection, error) {
	var c models.BankFeedConnection
	err := r.pool.QueryRow(ctx,
		`SELECT connection_id, provider, status,
		        COALESCE(institution_id,''), COALESCE(institution_name,''),
		        COALESCE(external_reference,''), COALESCE(auth_url,''),
		        COALESCE(last_error,''), last_synced_at,
		        created_at, updated_at
		 FROM bank_feed_connections
		 WHERE organisation_id = $1 AND connection_id = $2`, orgID, connID).Scan(
		&c.ConnectionID, &c.Provider, &c.Status,
		&c.InstitutionID, &c.InstitutionName,
		&c.ExternalReference, &c.AuthURL,
		&c.LastError, &c.LastSyncedAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	accs, err := r.listAccountsByConnection(ctx, connID)
	if err != nil {
		return nil, err
	}
	c.Accounts = accs
	return &c, nil
}

// DeleteConnection hard-deletes a connection and everything beneath it
// (accounts + statement lines) via ON DELETE CASCADE.
func (r *BankFeedRepository) DeleteConnection(ctx context.Context, orgID, connID uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`DELETE FROM bank_feed_connections WHERE organisation_id = $1 AND connection_id = $2`,
		orgID, connID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertAccount stores/updates a discovered account. ON CONFLICT keeps the
// ledger account binding (AccountID) stable across re-syncs.
func (r *BankFeedRepository) UpsertAccount(ctx context.Context, orgID uuid.UUID, a *models.BankFeedAccount) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO bank_feed_accounts
			(connection_id, organisation_id, external_account_id,
			 display_name, iban, currency_code, balance)
		 VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7)
		 ON CONFLICT (connection_id, external_account_id) DO UPDATE SET
			display_name  = COALESCE(EXCLUDED.display_name, bank_feed_accounts.display_name),
			iban          = COALESCE(EXCLUDED.iban,         bank_feed_accounts.iban),
			currency_code = COALESCE(EXCLUDED.currency_code,bank_feed_accounts.currency_code),
			balance       = EXCLUDED.balance,
			updated_at    = now()
		 RETURNING feed_account_id, updated_at`,
		a.ConnectionID, orgID, a.ExternalAccountID,
		a.DisplayName, a.IBAN, a.CurrencyCode, a.Balance,
	).Scan(&a.FeedAccountID, &a.UpdatedAt)
}

// BindAccount links a feed account to one of our internal BANK accounts so
// subsequent imports can land under the right ledger line.
func (r *BankFeedRepository) BindAccount(ctx context.Context, orgID, feedAccountID uuid.UUID, accountID *uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_accounts SET account_id = $3, updated_at = now()
		 WHERE organisation_id = $1 AND feed_account_id = $2`,
		orgID, feedAccountID, accountID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *BankFeedRepository) listAccountsByConnection(ctx context.Context, connID uuid.UUID) ([]models.BankFeedAccount, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT feed_account_id, connection_id, account_id, external_account_id,
		        COALESCE(display_name,''), COALESCE(iban,''),
		        COALESCE(currency_code,''), balance, updated_at
		 FROM bank_feed_accounts
		 WHERE connection_id = $1
		 ORDER BY created_at`, connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BankFeedAccount
	for rows.Next() {
		var a models.BankFeedAccount
		var bal *decimal.Decimal
		if err := rows.Scan(
			&a.FeedAccountID, &a.ConnectionID, &a.AccountID, &a.ExternalAccountID,
			&a.DisplayName, &a.IBAN, &a.CurrencyCode, &bal, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		a.Balance = bal
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertStatementLine is the idempotent ingestion point: sync loops push rows
// here and dedup happens on (feed_account_id, provider_tx_id).
// Returns true when the row was newly inserted.
func (r *BankFeedRepository) UpsertStatementLine(ctx context.Context, orgID, feedAccountID uuid.UUID, s *models.BankFeedStatementLine, raw []byte) (inserted bool, err error) {
	err = r.pool.QueryRow(ctx,
		`INSERT INTO bank_feed_statement_lines
			(organisation_id, feed_account_id, provider_tx_id, posted_at,
			 amount, currency_code, description, counterparty, reference, raw)
		 VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10)
		 ON CONFLICT (feed_account_id, provider_tx_id) DO UPDATE SET
			posted_at     = EXCLUDED.posted_at,
			amount        = EXCLUDED.amount,
			currency_code = EXCLUDED.currency_code,
			description   = COALESCE(EXCLUDED.description,   bank_feed_statement_lines.description),
			counterparty  = COALESCE(EXCLUDED.counterparty,  bank_feed_statement_lines.counterparty),
			reference     = COALESCE(EXCLUDED.reference,     bank_feed_statement_lines.reference),
			raw           = COALESCE(EXCLUDED.raw,           bank_feed_statement_lines.raw)
		 RETURNING statement_line_id, created_at, (xmax = 0) AS inserted`,
		orgID, feedAccountID, s.ProviderTxID, s.PostedAt,
		s.Amount, s.CurrencyCode, s.Description, s.Counterparty, s.Reference, raw,
	).Scan(&s.StatementLineID, &s.CreatedAt, &inserted)
	return inserted, err
}

// ListStatementLines is the feed inbox. Callers can narrow by status (NEW by
// default) or by feed account.
func (r *BankFeedRepository) ListStatementLines(ctx context.Context, orgID uuid.UUID, feedAccountID *uuid.UUID, status string, p models.Pagination) ([]models.BankFeedStatementLine, int, error) {
	args := []any{orgID}
	where := " WHERE organisation_id = $1"
	if feedAccountID != nil {
		args = append(args, *feedAccountID)
		where += " AND feed_account_id = $2"
	}
	if status != "" {
		args = append(args, status)
		where += " AND status = $" + strconv.Itoa(len(args))
	}
	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bank_feed_statement_lines"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	q := `SELECT statement_line_id, feed_account_id, provider_tx_id, posted_at,
	             amount, currency_code, COALESCE(description,''),
	             COALESCE(counterparty,''), COALESCE(reference,''),
	             status, bank_transaction_id, imported_at, created_at
	      FROM bank_feed_statement_lines` + where +
		" ORDER BY posted_at DESC, created_at DESC" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.BankFeedStatementLine
	for rows.Next() {
		var s models.BankFeedStatementLine
		if err := rows.Scan(
			&s.StatementLineID, &s.FeedAccountID, &s.ProviderTxID, &s.PostedAt,
			&s.Amount, &s.CurrencyCode, &s.Description,
			&s.Counterparty, &s.Reference,
			&s.Status, &s.BankTransactionID, &s.ImportedAt, &s.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// MarkLineImported flips a staging line to IMPORTED and records which
// bank_transaction it produced so re-imports don't double-book.
func (r *BankFeedRepository) MarkLineImported(ctx context.Context, orgID, lineID, bankTxID uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_statement_lines
		 SET status = 'IMPORTED', bank_transaction_id = $3, imported_at = now()
		 WHERE organisation_id = $1 AND statement_line_id = $2 AND status <> 'IMPORTED'`,
		orgID, lineID, bankTxID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkLineIgnored lets users skip rows they don't want to post (internal
// transfers, test payments etc.).
func (r *BankFeedRepository) MarkLineIgnored(ctx context.Context, orgID, lineID uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_statement_lines SET status = 'IGNORED'
		 WHERE organisation_id = $1 AND statement_line_id = $2 AND status = 'NEW'`,
		orgID, lineID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetStatementLine loads one staging row scoped to the tenant.
func (r *BankFeedRepository) GetStatementLine(ctx context.Context, orgID, lineID uuid.UUID) (*models.BankFeedStatementLine, error) {
	var s models.BankFeedStatementLine
	err := r.pool.QueryRow(ctx,
		`SELECT statement_line_id, feed_account_id, provider_tx_id, posted_at,
		        amount, currency_code, COALESCE(description,''),
		        COALESCE(counterparty,''), COALESCE(reference,''),
		        status, bank_transaction_id, imported_at, created_at
		 FROM bank_feed_statement_lines
		 WHERE organisation_id = $1 AND statement_line_id = $2`, orgID, lineID).Scan(
		&s.StatementLineID, &s.FeedAccountID, &s.ProviderTxID, &s.PostedAt,
		&s.Amount, &s.CurrencyCode, &s.Description,
		&s.Counterparty, &s.Reference,
		&s.Status, &s.BankTransactionID, &s.ImportedAt, &s.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// GetFeedAccount is used by the import handler to resolve a line back to its
// mapped ledger account.
func (r *BankFeedRepository) GetFeedAccount(ctx context.Context, orgID, feedAccountID uuid.UUID) (*models.BankFeedAccount, error) {
	var a models.BankFeedAccount
	var bal *decimal.Decimal
	err := r.pool.QueryRow(ctx,
		`SELECT feed_account_id, connection_id, account_id, external_account_id,
		        COALESCE(display_name,''), COALESCE(iban,''),
		        COALESCE(currency_code,''), balance, updated_at
		 FROM bank_feed_accounts
		 WHERE organisation_id = $1 AND feed_account_id = $2`, orgID, feedAccountID).Scan(
		&a.FeedAccountID, &a.ConnectionID, &a.AccountID, &a.ExternalAccountID,
		&a.DisplayName, &a.IBAN, &a.CurrencyCode, &bal, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.Balance = bal
	return &a, nil
}
