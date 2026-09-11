package repository

import (
	"context"
	"errors"
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
// last_error / last_synced_at. A blank lastError clears any previous error
// (success paths pass ""), while a non-blank one records why the feed failed.
func (r *BankFeedRepository) UpdateConnectionStatus(ctx context.Context, orgID, connID uuid.UUID, status, lastError string, syncedAt *time.Time) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET status         = $3,
		     last_error     = NULLIF($4, ''),
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
	// One grouped query for every connection's accounts instead of one query
	// per connection (N+1).
	ids := make([]uuid.UUID, len(out))
	for i := range out {
		ids[i] = out[i].ConnectionID
	}
	accounts, err := r.listAccountsByConnections(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Accounts = accounts[out[i].ConnectionID]
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
	accs, err := r.listAccountsByConnections(ctx, []uuid.UUID{connID})
	if err != nil {
		return nil, err
	}
	c.Accounts = accs[connID]
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

// listAccountsByConnections loads the accounts for several connections in one
// query, keyed by connection id, so list screens avoid an N+1.
func (r *BankFeedRepository) listAccountsByConnections(ctx context.Context, connIDs []uuid.UUID) (map[uuid.UUID][]models.BankFeedAccount, error) {
	out := make(map[uuid.UUID][]models.BankFeedAccount, len(connIDs))
	if len(connIDs) == 0 {
		return out, nil
	}
	ids := make([]string, len(connIDs))
	for i, id := range connIDs {
		ids[i] = id.String()
	}
	rows, err := r.pool.Query(ctx,
		`SELECT feed_account_id, connection_id, account_id, external_account_id,
		        COALESCE(display_name,''), COALESCE(iban,''),
		        COALESCE(currency_code,''), balance, updated_at
		 FROM bank_feed_accounts
		 WHERE connection_id = ANY($1::uuid[])
		 ORDER BY created_at`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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
		out[a.ConnectionID] = append(out[a.ConnectionID], a)
	}
	return out, rows.Err()
}

// UpsertStatementLine is the idempotent ingestion point for the sync loop:
// rows are keyed on (feed_account_id, provider_tx_id) so re-syncing a window
// updates rather than duplicates. Returns true when the row was new.
func (r *BankFeedRepository) UpsertStatementLine(ctx context.Context, orgID, feedAccountID uuid.UUID, s *models.BankStatementLine, raw []byte) (inserted bool, err error) {
	err = r.pool.QueryRow(ctx,
		`INSERT INTO bank_statement_lines
			(organisation_id, feed_account_id, source, provider_tx_id, posted_at,
			 amount, currency_code, payee, description, counterparty, reference, raw)
		 VALUES ($1,$2,'FEED',$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),$11)
		 ON CONFLICT (feed_account_id, provider_tx_id) DO UPDATE SET
			posted_at     = EXCLUDED.posted_at,
			amount        = EXCLUDED.amount,
			currency_code = EXCLUDED.currency_code,
			payee         = COALESCE(EXCLUDED.payee,        bank_statement_lines.payee),
			description   = COALESCE(EXCLUDED.description,  bank_statement_lines.description),
			counterparty  = COALESCE(EXCLUDED.counterparty, bank_statement_lines.counterparty),
			reference     = COALESCE(EXCLUDED.reference,    bank_statement_lines.reference),
			raw           = COALESCE(EXCLUDED.raw,          bank_statement_lines.raw)
		 RETURNING statement_line_id, created_at, (xmax = 0) AS inserted`,
		orgID, feedAccountID, s.ProviderTxID, s.PostedAt,
		s.Amount, s.CurrencyCode, s.Payee, s.Description, s.Counterparty, s.Reference, raw,
	).Scan(&s.StatementLineID, &s.CreatedAt, &inserted)
	return inserted, err
}

// ScheduledConnection is one row of the background sync sweep: which tenant
// owns a connection and what has to be pulled for it.
type ScheduledConnection struct {
	OrganisationID uuid.UUID
	ConnectionID   uuid.UUID
	Provider       string
}

// ListLinkedConnections returns every connection that is ready to sync, across
// all tenants. It backs the background poller, which has no request and
// therefore no tenant of its own.
func (r *BankFeedRepository) ListLinkedConnections(ctx context.Context) ([]ScheduledConnection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT organisation_id, connection_id, provider
		 FROM bank_feed_connections
		 WHERE status = $1
		 ORDER BY last_synced_at NULLS FIRST`, models.BankFeedStatusLinked)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduledConnection
	for rows.Next() {
		var c ScheduledConnection
		if err := rows.Scan(&c.OrganisationID, &c.ConnectionID, &c.Provider); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
