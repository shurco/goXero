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
			 country, external_reference, auth_url)
		 VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,''))
		 RETURNING connection_id, created_at, updated_at`,
		orgID, c.Provider, c.Status, c.InstitutionID, c.InstitutionName,
		c.Country, c.ExternalReference, c.AuthURL,
	).Scan(&c.ConnectionID, &c.CreatedAt, &c.UpdatedAt)
}

// LiveConnectionAtInstitution returns the tenant's live connection to an
// institution — PENDING (a consent in flight) or LINKED (a feed that works) —
// when it has one, and ErrNotFound when it does not. ERROR and REVOKED rows are
// deliberately not "live": those are repaired in place or replaced by a fresh
// consent, which is a different act from adding a second feed to a bank that
// already has a working one.
//
// `except` is the connection to leave out of the answer, which is how the
// finalize path asks "is somebody *else* already at this bank?" about the row it
// is in the middle of finishing.
func (r *BankFeedRepository) LiveConnectionAtInstitution(ctx context.Context, orgID uuid.UUID, provider, institutionID string, except uuid.UUID) (*models.BankFeedConnection, error) {
	var c models.BankFeedConnection
	err := r.pool.QueryRow(ctx,
		`SELECT connection_id, provider, status,
		        COALESCE(institution_id,''), COALESCE(institution_name,''), created_at
		 FROM bank_feed_connections
		 WHERE organisation_id = $1
		   AND provider        = $2
		   AND institution_id  = $3
		   AND status          = ANY($4)
		   AND connection_id  <> $5
		 ORDER BY created_at
		 LIMIT 1`,
		orgID, provider, institutionID,
		[]string{models.BankFeedStatusPending, models.BankFeedStatusLinked}, except,
	).Scan(&c.ConnectionID, &c.Provider, &c.Status,
		&c.InstitutionID, &c.InstitutionName, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
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
		        COALESCE(country,''),
		        COALESCE(external_reference,''), COALESCE(auth_url,''),
		        COALESCE(last_error,''), last_synced_at,
		        external_secret, COALESCE(sync_cursor,''), COALESCE(session_ref,''),
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
			&c.InstitutionID, &c.InstitutionName, &c.Country,
			&c.ExternalReference, &c.AuthURL,
			&c.LastError, &c.LastSyncedAt,
			&c.ExternalSecret, &c.SyncCursor, &c.SessionRef,
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
		        COALESCE(country,''),
		        COALESCE(external_reference,''), COALESCE(auth_url,''),
		        COALESCE(last_error,''), last_synced_at,
		        external_secret, COALESCE(sync_cursor,''), COALESCE(session_ref,''),
		        created_at, updated_at
		 FROM bank_feed_connections
		 WHERE organisation_id = $1 AND connection_id = $2`, orgID, connID).Scan(
		&c.ConnectionID, &c.Provider, &c.Status,
		&c.InstitutionID, &c.InstitutionName, &c.Country,
		&c.ExternalReference, &c.AuthURL,
		&c.LastError, &c.LastSyncedAt,
		&c.ExternalSecret, &c.SyncCursor, &c.SessionRef,
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

// SetConnectionSession records the consent session a connection is waiting on:
// the provider's session handle (Plaid link token, GoCardless requisition id)
// plus the URL the user finishes it at. It is how a PENDING connection is born,
// and how a broken one is sent back for repair.
func (r *BankFeedRepository) SetConnectionSession(ctx context.Context, orgID, connID uuid.UUID, sessionRef, authURL string) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET session_ref = NULLIF($3,''),
		     auth_url    = NULLIF($4,''),
		     updated_at  = now()
		 WHERE organisation_id = $1 AND connection_id = $2`,
		orgID, connID, sessionRef, authURL)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetConnectionConsent persists what a finished consent flow produced: the
// durable provider reference (Plaid item_id, GoCardless requisition_id) and the
// sealed per-connection secret — and clears the session, which is now over. A
// nil secret leaves any stored one untouched, so a provider that returns nothing
// to seal cannot wipe a real token by accident.
func (r *BankFeedRepository) SetConnectionConsent(ctx context.Context, orgID, connID uuid.UUID, externalReference string, secret []byte) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET external_reference = COALESCE(NULLIF($3,''), external_reference),
		     external_secret    = COALESCE($4, external_secret),
		     session_ref        = NULL,
		     auth_url           = NULL,
		     updated_at         = now()
		 WHERE organisation_id = $1 AND connection_id = $2`,
		orgID, connID, externalReference, secret)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetConnectionInstitution records the institution the provider says the
// connection is actually at, filling in a blank rather than clearing anything: a
// provider that reports only an id keeps the name we already had for it, and one
// that reports nothing at all leaves the row alone (the caller then never calls
// this). It is what keeps the row honest when the user chose the bank inside the
// provider's own picker rather than in our catalogue.
func (r *BankFeedRepository) SetConnectionInstitution(ctx context.Context, orgID, connID uuid.UUID, institutionID, institutionName string) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET institution_id   = COALESCE(NULLIF($3,''), institution_id),
		     institution_name = COALESCE(NULLIF($4,''), institution_name),
		     updated_at       = now()
		 WHERE organisation_id = $1 AND connection_id = $2`,
		orgID, connID, institutionID, institutionName)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ConnectionBySessionRef resolves the connection a provider session belongs to —
// how a Plaid SESSION_FINISHED webhook, which knows its link token and nothing
// else, finds the row it is about.
//
// Webhook deliveries arrive with no tenant header, so this is the one lookup that
// has to cross organisations: it hands back the owning organisation, and every
// write that follows stays scoped to it as usual. A session handle is unique
// across the provider, so the lookup needs nothing else. The `status <> 'LINKED'`
// guard is what makes the webhook and the browser returning from consent settle
// the same session rather than race: whichever arrives second finds no session
// in flight and leaves well alone.
func (r *BankFeedRepository) ConnectionBySessionRef(ctx context.Context, sessionRef string) (orgID, connID uuid.UUID, err error) {
	if sessionRef == "" {
		return uuid.Nil, uuid.Nil, ErrNotFound
	}
	err = r.pool.QueryRow(ctx,
		`SELECT organisation_id, connection_id
		 FROM bank_feed_connections
		 WHERE session_ref = $1 AND status <> $2`,
		sessionRef, models.BankFeedStatusLinked).Scan(&orgID, &connID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, uuid.Nil, ErrNotFound
		}
		return uuid.Nil, uuid.Nil, err
	}
	return orgID, connID, nil
}

// ConnectionByExternalReference resolves a connection from the provider's own
// durable id (Plaid's item_id), which is what an out-of-band notification about
// an Item names. Like the session lookup it crosses organisations, because
// webhooks arrive without a tenant header, and hands back the owner so the
// writes that follow stay scoped.
//
// Providers issue these ids across the whole account rather than per tenant, so
// one reference belongs to one connection; the ordering only decides which row
// wins if that ever stops being true.
func (r *BankFeedRepository) ConnectionByExternalReference(ctx context.Context, provider, externalReference string) (orgID, connID uuid.UUID, err error) {
	if externalReference == "" {
		return uuid.Nil, uuid.Nil, ErrNotFound
	}
	err = r.pool.QueryRow(ctx,
		`SELECT organisation_id, connection_id
		 FROM bank_feed_connections
		 WHERE provider = $1 AND external_reference = $2
		 ORDER BY updated_at DESC
		 LIMIT 1`,
		provider, externalReference).Scan(&orgID, &connID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, uuid.Nil, ErrNotFound
		}
		return uuid.Nil, uuid.Nil, err
	}
	return orgID, connID, nil
}

// NoteConnectionWarning records something the user should know about a working
// connection without changing its status — a consent that lapses in a week, or a
// sync that failed for a reason that leaves the feed usable. The row stays
// LINKED, so the feed keeps syncing and the user is not sent to repair a consent
// that is still good. A later successful sync clears it, which is deliberate for
// a lapse warning and good enough for a failure the next sweep will retry.
func (r *BankFeedRepository) NoteConnectionWarning(ctx context.Context, orgID, connID uuid.UUID, message string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET last_error = NULLIF($3,''), updated_at = now()
		 WHERE organisation_id = $1 AND connection_id = $2 AND status = $4`,
		orgID, connID, message, models.BankFeedStatusLinked)
	return err
}

// FailFinalize records why completing a consent failed — unless the connection
// has been linked meanwhile. The browser redirect and the SESSION_FINISHED
// webhook both finish the same session, so the loser of that race must not undo
// the winner's success.
func (r *BankFeedRepository) FailFinalize(ctx context.Context, orgID, connID uuid.UUID, lastError string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET status = $3, last_error = NULLIF($4,''), updated_at = now()
		 WHERE organisation_id = $1 AND connection_id = $2 AND status <> $5`,
		orgID, connID, models.BankFeedStatusError, lastError, models.BankFeedStatusLinked)
	return err
}

// FailLinkedConnection records an upstream failure reported out of band on a
// connection that was working (a Plaid ITEM ERROR webhook, say). A connection
// still PENDING is left alone: its consent flow is in flight and will decide for
// itself what the outcome is.
func (r *BankFeedRepository) FailLinkedConnection(ctx context.Context, orgID, connID uuid.UUID, lastError string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET status = $3, last_error = NULLIF($4,''), updated_at = now()
		 WHERE organisation_id = $1 AND connection_id = $2 AND status = $5`,
		orgID, connID, models.BankFeedStatusError, lastError, models.BankFeedStatusLinked)
	return err
}

// RevokeConnection marks a consent the user has revoked at the bank. The rows we
// already staged stay: they are part of the books now, and only the feed stops.
func (r *BankFeedRepository) RevokeConnection(ctx context.Context, orgID, connID uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET status = $3, last_error = NULLIF($4,''), updated_at = now()
		 WHERE organisation_id = $1 AND connection_id = $2`,
		orgID, connID, models.BankFeedStatusRevoked, reason)
	return err
}

// SetSyncCursor records how far the connection has been synced so the next
// incremental sync resumes from there instead of re-reading history.
func (r *BankFeedRepository) SetSyncCursor(ctx context.Context, orgID, connID uuid.UUID, cursor string) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_feed_connections
		 SET sync_cursor = NULLIF($3,''), updated_at = now()
		 WHERE organisation_id = $1 AND connection_id = $2`,
		orgID, connID, cursor)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ApplyRemovedLines reconciles the provider's `removed` list with what we have
// staged (Plaid reports there the transactions the bank has dropped — a pending
// charge, typically, whose posted replacement arrives under a fresh id).
//
// What happens to a listed line depends on how far the user got with it. One
// still in the inbox simply goes: the bank withdrew it and nobody has acted on
// it. One that has been coded is part of the books, so it stays and is marked as
// withdrawn instead — deleting it would silently pull a booked entry out of the
// ledger and leave the bank transaction it became orphaned.
//
// Both counts are returned so a sync can say what it did.
func (r *BankFeedRepository) ApplyRemovedLines(ctx context.Context, orgID, connID uuid.UUID, providerTxIDs []string) (removed, withdrawn int64, err error) {
	if len(providerTxIDs) == 0 {
		return 0, 0, nil
	}
	err = r.pool.QueryRow(ctx,
		`WITH withdrawn AS (
		    UPDATE bank_statement_lines l
		       SET upstream_removed_at = now()
		      FROM bank_feed_accounts fa
		     WHERE l.feed_account_id = fa.feed_account_id
		       AND fa.connection_id = $2
		       AND l.organisation_id = $1
		       AND l.provider_tx_id = ANY($3)
		       AND l.status NOT IN ('NEW','IGNORED')
		       AND l.upstream_removed_at IS NULL
		    RETURNING l.statement_line_id
		 ), dropped AS (
		    DELETE FROM bank_statement_lines l
		     USING bank_feed_accounts fa
		     WHERE l.feed_account_id = fa.feed_account_id
		       AND fa.connection_id = $2
		       AND l.organisation_id = $1
		       AND l.provider_tx_id = ANY($3)
		       AND l.status IN ('NEW','IGNORED')
		    RETURNING l.statement_line_id
		 )
		 SELECT (SELECT count(*) FROM dropped), (SELECT count(*) FROM withdrawn)`,
		orgID, connID, providerTxIDs).Scan(&removed, &withdrawn)
	return removed, withdrawn, err
}

// CountUpstreamChanges counts the lines under a connection the bank disagrees
// with — restated after they were coded, or withdrawn outright — so a sync can
// tell the user there is something to look at rather than leaving them to find
// the notice in the inbox.
func (r *BankFeedRepository) CountUpstreamChanges(ctx context.Context, orgID, connID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*)
		 FROM bank_statement_lines l
		 JOIN bank_feed_accounts fa ON fa.feed_account_id = l.feed_account_id
		 WHERE l.organisation_id = $1
		   AND fa.connection_id = $2
		   AND `+upstreamChangeExpr+` IS NOT NULL`,
		orgID, connID).Scan(&n)
	return n, err
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

// ProvisionLedgerAccount creates the ledger account a feed account reconciles
// against and binds it, returning the account id. This is what puts a connected
// bank on the bank accounts screen: that screen lists ledger accounts, and a
// consent on its own only stages rows in bank_feed_accounts.
//
// One transaction, and the feed account is claimed with FOR UPDATE first. The
// completion can arrive twice at once — Plaid's SESSION_FINISHED webhook and the
// browser returning from consent both finish the same session — and two ledger
// accounts for one bank account is not something the user can untangle later.
// A feed account that already has one keeps it, so a re-finalize, a repair or a
// sync that discovers nothing new all leave the chart alone.
func (r *BankFeedRepository) ProvisionLedgerAccount(ctx context.Context, orgID, feedAccountID uuid.UUID) (uuid.UUID, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		bound    *uuid.UUID
		name     string
		currency string
	)
	err = tx.QueryRow(ctx,
		`SELECT account_id,
		        LEFT(COALESCE(NULLIF(display_name,''), external_account_id), 150),
		        COALESCE(currency_code,'')
		 FROM bank_feed_accounts
		 WHERE organisation_id=$1 AND feed_account_id=$2
		 FOR UPDATE`,
		orgID, feedAccountID).Scan(&bound, &name, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if bound != nil {
		return *bound, nil
	}

	code, err := nextBankAccountCode(ctx, tx, orgID)
	if err != nil {
		return uuid.Nil, err
	}
	var accountID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO accounts
			(organisation_id, code, name, type, bank_account_type, currency_code, status, class)
		 VALUES ($1,$2,$3,'BANK','BANK',
			COALESCE(NULLIF($4,''), (SELECT base_currency FROM organisations WHERE organisation_id=$1)),
			'ACTIVE',$5)
		 RETURNING account_id`,
		orgID, code, name, currency, models.AccountClassForType(models.AccountTypeBank),
	).Scan(&accountID)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE bank_feed_accounts SET account_id=$3, updated_at=now()
		 WHERE organisation_id=$1 AND feed_account_id=$2`,
		orgID, feedAccountID, accountID); err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return accountID, nil
}

// nextBankAccountCode continues the organisation's own bank-account series — a
// chart holding 090 and 091 gets 092 — rather than inventing a range of its own,
// and skips codes already taken because (organisation_id, code) is unique. A
// chart with no bank account yet starts the series at 090.
func nextBankAccountCode(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (string, error) {
	var code string
	err := tx.QueryRow(ctx,
		`WITH series AS (
			SELECT COALESCE(MAX(NULLIF(regexp_replace(code,'\D','','g'),'')::int), 89) + 1 AS start,
			       COALESCE(MAX(length(code)), 3) AS width
			FROM accounts
			WHERE organisation_id=$1 AND type='BANK' AND code ~ '^[0-9]+$'
		)
		SELECT LPAD(n::text, series.width, '0')
		FROM series, generate_series(series.start, series.start + 999) AS n
		WHERE NOT EXISTS (
			SELECT 1 FROM accounts a
			WHERE a.organisation_id=$1 AND a.code = LPAD(n::text, series.width, '0')
		)
		ORDER BY n
		LIMIT 1`,
		orgID).Scan(&code)
	return code, err
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
	rows, err := r.pool.Query(ctx,
		`SELECT feed_account_id, connection_id, account_id, external_account_id,
		        COALESCE(display_name,''), COALESCE(iban,''),
		        COALESCE(currency_code,''), balance, updated_at
		 FROM bank_feed_accounts
		 WHERE connection_id = ANY($1::uuid[])
		 ORDER BY created_at`, connIDs)
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

// statementLineIsInbox and statementLineIsBooked split the line statuses in two:
// a line the user has not acted on (NEW, or IGNORED — an answer in itself, and
// still not the books) and one that has left the inbox for a bank transaction
// (IMPORTED, or PROCESSING while that transaction is being created). The split is
// what decides whether the bank's newest version of a line may be written over
// what we hold.
const (
	statementLineIsInbox  = `bank_statement_lines.status IN ('NEW','IGNORED')`
	statementLineIsBooked = `bank_statement_lines.status NOT IN ('NEW','IGNORED')`
)

// upsertStatementLineSQL is the ingestion query. It reads as three rules:
//
//   - while a line is still in the inbox the bank's newer version simply
//     replaces ours; that is the pending charge whose amount settles before it
//     posts, and the reason a sync is not append-only;
//   - once a line has been booked, the bank's version is parked in the
//     upstream_* columns instead. The user has decided what this line is, and a
//     feed has no business rewriting the ledger behind their back;
//   - the notice is derived on read from those columns (see upstreamChangeExpr),
//     so it needs no maintenance of its own and clears itself the moment the two
//     versions agree again — which is also why upstream_changed_at only moves
//     when the bank's version actually changes.
var upsertStatementLineSQL = `
	INSERT INTO bank_statement_lines
		(organisation_id, feed_account_id, source, provider_tx_id, posted_at,
		 amount, currency_code, payee, description, counterparty, reference, raw)
	VALUES ($1,$2,'FEED',$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),$11)
	ON CONFLICT (feed_account_id, provider_tx_id) DO UPDATE SET
		posted_at     = CASE WHEN ` + statementLineIsInbox + ` THEN EXCLUDED.posted_at     ELSE bank_statement_lines.posted_at     END,
		amount        = CASE WHEN ` + statementLineIsInbox + ` THEN EXCLUDED.amount        ELSE bank_statement_lines.amount        END,
		currency_code = CASE WHEN ` + statementLineIsInbox + ` THEN EXCLUDED.currency_code ELSE bank_statement_lines.currency_code END,
		payee         = COALESCE(EXCLUDED.payee,        bank_statement_lines.payee),
		description   = COALESCE(EXCLUDED.description,  bank_statement_lines.description),
		counterparty  = COALESCE(EXCLUDED.counterparty, bank_statement_lines.counterparty),
		reference     = COALESCE(EXCLUDED.reference,    bank_statement_lines.reference),
		raw           = COALESCE(EXCLUDED.raw,          bank_statement_lines.raw),
		upstream_amount    = CASE WHEN ` + statementLineIsBooked + ` THEN EXCLUDED.amount    ELSE NULL END,
		upstream_posted_at = CASE WHEN ` + statementLineIsBooked + ` THEN EXCLUDED.posted_at ELSE NULL END,
		upstream_changed_at = CASE
			WHEN ` + statementLineIsBooked + `
			 AND (bank_statement_lines.upstream_amount, bank_statement_lines.upstream_posted_at)
			     IS DISTINCT FROM (EXCLUDED.amount, EXCLUDED.posted_at)
			THEN now()
			WHEN ` + statementLineIsBooked + ` THEN bank_statement_lines.upstream_changed_at
			ELSE NULL END,
		upstream_removed_at = CASE WHEN ` + statementLineIsBooked + ` THEN bank_statement_lines.upstream_removed_at ELSE NULL END
	 RETURNING statement_line_id, created_at, (xmax = 0) AS inserted`

// UpsertStatementLine is the idempotent ingestion point for the sync loop: rows
// are keyed on (feed_account_id, provider_tx_id) so re-syncing a window updates
// rather than duplicates. Returns true when the row was new.
func (r *BankFeedRepository) UpsertStatementLine(ctx context.Context, orgID, feedAccountID uuid.UUID, s *models.BankStatementLine, raw []byte) (inserted bool, err error) {
	err = r.pool.QueryRow(ctx, upsertStatementLineSQL,
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
