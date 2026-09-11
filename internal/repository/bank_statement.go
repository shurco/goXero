package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

// BankStatementRepository owns the reconcile inbox: the statement lines that
// arrive from an Open Banking feed or from a manual file import, the import
// batches themselves, and the reconcile periods that lock finished ranges.
//
// Feed ingestion (connections, accounts, the provider sync loop) stays in
// BankFeedRepository; everything downstream of a line existing is here, because
// Xero treats a feed line and an imported line identically once it is in the
// inbox.
type BankStatementRepository struct {
	pool *pgxpool.Pool
}

// statementLineColumns is the canonical projection for a statement line. It is
// shared by every read path so a new column only has to be added once.
// A feed line has no bank_account_id of its own — it hangs off a feed account,
// which is bound to a ledger account separately, and often after the lines
// arrived. The projection resolves it so every caller sees which ledger account
// a line belongs to, whichever source it came from.
const statementLineColumns = `
	l.statement_line_id, l.feed_account_id,
	COALESCE(l.bank_account_id,
	         (SELECT fa.account_id FROM bank_feed_accounts fa
	          WHERE fa.feed_account_id = l.feed_account_id)) AS bank_account_id,
	l.import_id, l.source,
	COALESCE(l.provider_tx_id,''), l.posted_at, l.amount, l.balance, COALESCE(l.currency_code,''),
	COALESCE(l.payee,''), COALESCE(l.description,''), COALESCE(l.counterparty,''),
	COALESCE(l.reference,''), COALESCE(l.cheque_number,''), l.status, l.bank_transaction_id,
	l.coded_at, l.coded_by, l.imported_at, l.created_at`

const statementLineFrom = ` FROM bank_statement_lines l`

func scanStatementLine(row pgx.Row) (*models.BankStatementLine, error) {
	s := &models.BankStatementLine{}
	err := row.Scan(
		&s.StatementLineID, &s.FeedAccountID, &s.BankAccountID, &s.ImportID, &s.Source,
		&s.ProviderTxID, &s.PostedAt, &s.Amount, &s.Balance, &s.CurrencyCode,
		&s.Payee, &s.Description, &s.Counterparty,
		&s.Reference, &s.ChequeNumber, &s.Status, &s.BankTransactionID,
		&s.CodedAt, &s.CodedBy, &s.ImportedAt, &s.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

// StatementLineFilter narrows the inbox. Zero values mean "no restriction".
type StatementLineFilter struct {
	FeedAccountID *uuid.UUID
	BankAccountID *uuid.UUID
	ImportID      *uuid.UUID
	Status        string
	Source        string
	Search        string
	From          *time.Time
	To            *time.Time
	// UnreconciledOnly is the inbox default: only lines still awaiting a
	// decision. It is expressed separately from Status because Xero's "show
	// unreconciled" also hides lines the user ignored.
	UnreconciledOnly bool
}

func (f StatementLineFilter) where() (string, []any) {
	var sb strings.Builder
	sb.WriteString(" WHERE l.organisation_id=$1")
	args := []any{}
	add := func(clause string, v any) {
		args = append(args, v)
		sb.WriteString(clause + "$" + strconv.Itoa(len(args)+1))
	}
	if f.FeedAccountID != nil {
		add(" AND l.feed_account_id=", *f.FeedAccountID)
	}
	if f.BankAccountID != nil {
		// A line belongs to the account either directly (IMPORT) or through its
		// feed account's binding (FEED), and the user thinks of both as "the
		// lines for this bank account".
		add(" AND (l.bank_account_id=", *f.BankAccountID)
		sb.WriteString(" OR l.feed_account_id IN (SELECT feed_account_id FROM bank_feed_accounts WHERE account_id=$" +
			strconv.Itoa(len(args)) + "))")
	}
	if f.ImportID != nil {
		add(" AND l.import_id=", *f.ImportID)
	}
	if f.Status != "" {
		add(" AND l.status=", f.Status)
	} else if f.UnreconciledOnly {
		sb.WriteString(" AND l.status='NEW'")
	}
	if f.Source != "" {
		add(" AND l.source=", f.Source)
	}
	if f.Search != "" {
		args = append(args, "%"+escapeLikePattern(f.Search)+"%")
		n := strconv.Itoa(len(args) + 1)
		sb.WriteString(" AND (COALESCE(l.payee,'') ILIKE $" + n +
			" OR COALESCE(l.description,'') ILIKE $" + n +
			" OR COALESCE(l.reference,'') ILIKE $" + n +
			" OR COALESCE(l.counterparty,'') ILIKE $" + n + ")")
	}
	if f.From != nil {
		add(" AND l.posted_at>=", *f.From)
	}
	if f.To != nil {
		add(" AND l.posted_at<=", *f.To)
	}
	return sb.String(), args
}

// escapeLikePattern neutralises LIKE metacharacters so a search for "50%"
// matches the literal text instead of every line.
func escapeLikePattern(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ListLines returns one page of the reconcile inbox plus the unpaged total.
func (r *BankStatementRepository) ListLines(ctx context.Context, orgID uuid.UUID, f StatementLineFilter, p models.Pagination) ([]models.BankStatementLine, int, error) {
	where, args := f.where()
	args = append([]any{orgID}, args...)

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bank_statement_lines l"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	q := "SELECT " + statementLineColumns + statementLineFrom + where +
		" ORDER BY posted_at DESC, created_at DESC" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.BankStatementLine
	for rows.Next() {
		s, err := scanStatementLine(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *s)
	}
	return out, total, rows.Err()
}

// GetLine loads one statement line. `bankAccountID` is resolved from the line's
// feed account when it came from a feed, so callers always have something to
// post against.
func (r *BankStatementRepository) GetLine(ctx context.Context, orgID, lineID uuid.UUID) (*models.BankStatementLine, error) {
	q := "SELECT " + statementLineColumns + statementLineFrom +
		" WHERE l.organisation_id=$1 AND l.statement_line_id=$2"
	return scanStatementLine(r.pool.QueryRow(ctx, q, orgID, lineID))
}

// rowQuerier is satisfied by both *pgxpool.Pool and a pgx.Tx, so a guard like
// requireAccountInOrg can run either standalone or inside a transaction.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// requireAccountInOrg refuses to touch a bank account that does not belong to
// the tenant, so a caller cannot attach an import or a reconcile period to
// another organisation's account by passing its UUID.
func (r *BankStatementRepository) requireAccountInOrg(ctx context.Context, q rowQuerier, orgID, accountID uuid.UUID) error {
	var ok bool
	if err := q.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM accounts WHERE organisation_id=$1 AND account_id=$2)`,
		orgID, accountID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// staleClaim is how long a claim may sit before another request may take it
// over. Long enough that a live request is never pre-empted, short enough that a
// line abandoned by a killed request returns to the inbox on its own.
const staleClaim = 5 * time.Minute

// ClaimLine takes a line out of the inbox so exactly one caller can turn it
// into a transaction. The conditional transition is the concurrency guard: two
// tabs on the same line race here and only the winner gets a row back, while the
// loser is told the line is already spoken for.
func (r *BankStatementRepository) ClaimLine(ctx context.Context, orgID, lineID uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_statement_lines SET status='PROCESSING', claimed_at=now()
		 WHERE organisation_id=$1 AND statement_line_id=$2
		   AND (status IN ('NEW','IGNORED')
		        OR (status='PROCESSING' AND claimed_at < $3))`,
		orgID, lineID, time.Now().Add(-staleClaim))
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrAlreadyExists
	}
	return nil
}

// AttachLineTransaction commits a claimed line: it records the transaction the
// line became (nil for a transfer, which posts both legs itself) and marks the
// line reconciled.
func (r *BankStatementRepository) AttachLineTransaction(ctx context.Context, orgID, lineID uuid.UUID, bankTxID *uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_statement_lines
		 SET status='IMPORTED', bank_transaction_id=$3, imported_at=now(), claimed_at=NULL
		 WHERE organisation_id=$1 AND statement_line_id=$2 AND status='PROCESSING'`,
		orgID, lineID, bankTxID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReleaseLine puts a claimed line back in the inbox when the transaction it was
// being turned into could not be created, so a failure never swallows a line.
func (r *BankStatementRepository) ReleaseLine(ctx context.Context, orgID, lineID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE bank_statement_lines SET status='NEW', claimed_at=NULL
		 WHERE organisation_id=$1 AND statement_line_id=$2 AND status='PROCESSING'`,
		orgID, lineID)
	return err
}

// SetLineStatus is the generic transition behind "ignore" and "unignore" in
// the inbox. Xero allows un-ignoring a line, so IGNORED must not be terminal.
func (r *BankStatementRepository) SetLineStatus(ctx context.Context, orgID, lineID uuid.UUID, status string) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_statement_lines SET status=$3
		 WHERE organisation_id=$1 AND statement_line_id=$2 AND status <> 'IMPORTED'`,
		orgID, lineID, status)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkLinesCoded stamps the "coded in cash coding" audit fields. Xero keeps
// who coded a line and when, which is what a reconcile period lock is enforced
// against.
func (r *BankStatementRepository) MarkLinesCoded(ctx context.Context, orgID uuid.UUID, lineIDs []uuid.UUID, userID *uuid.UUID) error {
	if len(lineIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE bank_statement_lines SET coded_at=now(), coded_by=$3
		 WHERE organisation_id=$1 AND statement_line_id=ANY($2::uuid[])`,
		orgID, lineIDs, userID)
	return err
}

// ExistingFingerprints returns the duplicate-detection keys already present for
// a bank account in a date window. The import wizard calls this once per file
// rather than per line, so a thousand-row statement is one query.
//
// The account predicate matches both sources: lines imported directly carry
// bank_account_id, while feed lines resolve through their feed account's
// binding — otherwise re-importing a statement whose rows already arrived from
// Open Banking would report no duplicates and double-book them.
func (r *BankStatementRepository) ExistingFingerprints(ctx context.Context, orgID, bankAccountID uuid.UUID, from, to time.Time) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT posted_at, amount, COALESCE(payee,''), COALESCE(reference,'')
		 FROM bank_statement_lines
		 WHERE organisation_id=$1
		   AND (bank_account_id=$2
		        OR feed_account_id IN (SELECT feed_account_id FROM bank_feed_accounts WHERE account_id=$2))
		   AND posted_at BETWEEN $3 AND $4`,
		orgID, bankAccountID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var (
			posted time.Time
			amount decimal.Decimal
			payee  string
			ref    string
		)
		if err := rows.Scan(&posted, &amount, &payee, &ref); err != nil {
			return nil, err
		}
		out[StatementFingerprint(posted, amount, payee, ref)] = true
	}
	return out, rows.Err()
}

// StatementFingerprint is the duplicate-detection key for a statement line: the
// date, amount, payee and reference, normalised so cosmetic differences do not
// hide a duplicate. It is exported so the import wizard computes the same key
// for a freshly parsed line and for one already stored.
func StatementFingerprint(posted time.Time, amount decimal.Decimal, payee, reference string) string {
	norm := func(s string) string {
		return strings.ToLower(strings.Join(strings.Fields(s), " "))
	}
	return posted.UTC().Format("2006-01-02") + "|" + amount.StringFixed(4) + "|" + norm(payee) + "|" + norm(reference)
}

// ---------------------------------------------------------------------------
// Manual statement imports
// ---------------------------------------------------------------------------

const statementImportColumns = `
	import_id, bank_account_id, COALESCE(filename,''), format, status,
	line_count, imported_count, duplicate_count, COALESCE(currency_code,''),
	statement_start, statement_end, opening_balance, closing_balance,
	COALESCE(mapping,'{}'::jsonb), COALESCE(last_error,''), created_at, committed_at`

func scanStatementImport(row pgx.Row) (*models.BankStatementImport, error) {
	imp := &models.BankStatementImport{}
	var mapping []byte
	var filename, currency, lastError string
	err := row.Scan(
		&imp.ImportID, &imp.BankAccountID, &filename, &imp.Format, &imp.Status,
		&imp.LineCount, &imp.ImportedCount, &imp.DuplicateCount, &currency,
		&imp.StatementStart, &imp.StatementEnd, &imp.OpeningBalance, &imp.ClosingBalance,
		&mapping, &lastError, &imp.CreatedAt, &imp.CommittedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	imp.Filename, imp.CurrencyCode, imp.LastError = filename, currency, lastError
	if len(mapping) > 0 && string(mapping) != "null" {
		if err := jsonUnmarshal(mapping, &imp.Mapping); err != nil {
			return nil, err
		}
	}
	return imp, nil
}

// CreateImport stages a parsed file: the rows land in `payload` and stay there
// until the user confirms the mapping in step 2, so no statement line is
// created until they press Import.
func (r *BankStatementRepository) CreateImport(ctx context.Context, orgID uuid.UUID, imp *models.BankStatementImport, payload []byte) error {
	if err := r.requireAccountInOrg(ctx, r.pool, orgID, imp.BankAccountID); err != nil {
		return err
	}
	return r.pool.QueryRow(ctx,
		`INSERT INTO bank_statement_imports
			(organisation_id, bank_account_id, filename, format, status,
			 line_count, currency_code, statement_start, statement_end,
			 opening_balance, closing_balance, mapping, payload)
		 VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11,$12::jsonb,$13::jsonb)
		 RETURNING import_id, created_at`,
		orgID, imp.BankAccountID, imp.Filename, imp.Format, imp.Status,
		imp.LineCount, imp.CurrencyCode, imp.StatementStart, imp.StatementEnd,
		imp.OpeningBalance, imp.ClosingBalance, mappingJSON(imp.Mapping), payload,
	).Scan(&imp.ImportID, &imp.CreatedAt)
}

// UpdateImport rewrites the staged import after the user changes the mapping in
// the wizard. Only STAGED imports can be re-mapped; a committed one is history.
func (r *BankStatementRepository) UpdateImport(ctx context.Context, orgID uuid.UUID, imp *models.BankStatementImport, payload []byte) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_statement_imports SET
			filename=NULLIF($3,''), format=$4, line_count=$5, currency_code=NULLIF($6,''),
			statement_start=$7, statement_end=$8, opening_balance=$9, closing_balance=$10,
			mapping=$11::jsonb, payload=$12::jsonb, last_error=NULL
		 WHERE organisation_id=$1 AND import_id=$2 AND status='STAGED'`,
		orgID, imp.ImportID, imp.Filename, imp.Format, imp.LineCount, imp.CurrencyCode,
		imp.StatementStart, imp.StatementEnd, imp.OpeningBalance, imp.ClosingBalance,
		mappingJSON(imp.Mapping), payload)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Payload returns the staged rows of an import so the wizard can preview them
// without re-uploading the file.
func (r *BankStatementRepository) Payload(ctx context.Context, orgID, importID uuid.UUID) ([]byte, error) {
	var payload []byte
	err := r.pool.QueryRow(ctx,
		`SELECT payload FROM bank_statement_imports WHERE organisation_id=$1 AND import_id=$2`,
		orgID, importID).Scan(&payload)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return payload, nil
}

func (r *BankStatementRepository) GetImport(ctx context.Context, orgID, importID uuid.UUID) (*models.BankStatementImport, error) {
	q := "SELECT " + statementImportColumns + " FROM bank_statement_imports" +
		" WHERE organisation_id=$1 AND import_id=$2"
	return scanStatementImport(r.pool.QueryRow(ctx, q, orgID, importID))
}

// ListImports returns the import history for an account — Xero's "Statement
// imports" list, which is also where a bad import gets undone.
func (r *BankStatementRepository) ListImports(ctx context.Context, orgID uuid.UUID, bankAccountID *uuid.UUID, p models.Pagination) ([]models.BankStatementImport, int, error) {
	where := " WHERE organisation_id=$1"
	args := []any{orgID}
	if bankAccountID != nil {
		args = append(args, *bankAccountID)
		where += " AND bank_account_id=$" + strconv.Itoa(len(args))
	}
	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bank_statement_imports"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	q := "SELECT " + statementImportColumns + " FROM bank_statement_imports" + where +
		" ORDER BY created_at DESC" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.BankStatementImport
	for rows.Next() {
		imp, err := scanStatementImport(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *imp)
	}
	return out, total, rows.Err()
}

// StatementLineInput is one row about to be written by CommitImport. It carries
// the pre-computed fingerprint so the duplicate check and the insert agree on
// what "the same line" means.
type StatementLineInput struct {
	PostedAt     time.Time
	Amount       decimal.Decimal
	Balance      *decimal.Decimal
	CurrencyCode string
	Payee        string
	Description  string
	Reference    string
	ChequeNumber string
	ProviderTxID string
	Fingerprint  string
	Duplicate    bool
}

// CommitImport materialises a staged import into statement lines. Rows the
// caller flagged as duplicates are skipped and counted instead of inserted, so
// re-importing an overlapping statement is safe. Everything happens in one
// transaction: a partially imported statement would be worse than none.
func (r *BankStatementRepository) CommitImport(ctx context.Context, orgID, importID uuid.UUID, lines []StatementLineInput) (imported, skipped int, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	var (
		bankAccountID uuid.UUID
		currency      string
	)
	if err := tx.QueryRow(ctx,
		`SELECT bank_account_id, COALESCE(currency_code,'') FROM bank_statement_imports
		 WHERE organisation_id=$1 AND import_id=$2 AND status='STAGED'`,
		orgID, importID).Scan(&bankAccountID, &currency); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, ErrNotFound
		}
		return 0, 0, err
	}
	if currency == "" {
		if err := tx.QueryRow(ctx,
			`SELECT COALESCE(currency_code,'') FROM accounts WHERE account_id=$1`,
			bankAccountID).Scan(&currency); err != nil {
			return 0, 0, err
		}
	}

	for _, l := range lines {
		if l.Duplicate {
			skipped++
			continue
		}
		ccy := l.CurrencyCode
		if ccy == "" {
			ccy = currency
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO bank_statement_lines
				(organisation_id, bank_account_id, import_id, source, provider_tx_id,
				 posted_at, amount, balance, currency_code, payee, description,
				 reference, cheque_number, status)
			 VALUES ($1,$2,$3,'IMPORT',$4,$5,$6,$7,$8,
				NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),'NEW')`,
			orgID, bankAccountID, importID, l.Fingerprint,
			l.PostedAt, l.Amount, l.Balance, ccy, l.Payee, l.Description,
			l.Reference, l.ChequeNumber); err != nil {
			return 0, 0, err
		}
		imported++
	}

	cmd, err := tx.Exec(ctx,
		`UPDATE bank_statement_imports
		 SET status='IMPORTED', imported_count=$3, duplicate_count=$4, committed_at=now()
		 WHERE organisation_id=$1 AND import_id=$2 AND status='STAGED'`,
		orgID, importID, imported, skipped)
	if err != nil {
		return 0, 0, err
	}
	if cmd.RowsAffected() == 0 {
		return 0, 0, ErrNotFound
	}
	return imported, skipped, tx.Commit(ctx)
}

// FailImport records why an import could not be committed so the wizard can
// show the user the file's problem instead of a blank failure.
func (r *BankStatementRepository) FailImport(ctx context.Context, orgID, importID uuid.UUID, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE bank_statement_imports SET status='FAILED', last_error=$3
		 WHERE organisation_id=$1 AND import_id=$2 AND status='STAGED'`,
		orgID, importID, reason)
	return err
}

// DeleteImport undoes an import: the statement lines it created go with it, but
// only while none of them has been reconciled into a bank transaction. Xero
// refuses to undo an import that has already been coded, and so do we — the
// caller turns ErrForbidden into "unreconcile those lines first".
func (r *BankStatementRepository) DeleteImport(ctx context.Context, orgID, importID uuid.UUID) (deleted int64, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var booked int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM bank_statement_lines
		 WHERE organisation_id=$1 AND import_id=$2 AND status='IMPORTED'`,
		orgID, importID).Scan(&booked); err != nil {
		return 0, err
	}
	if booked > 0 {
		return 0, ErrForbidden
	}
	cmd, err := tx.Exec(ctx,
		`DELETE FROM bank_statement_lines WHERE organisation_id=$1 AND import_id=$2`,
		orgID, importID)
	if err != nil {
		return 0, err
	}
	deleted = cmd.RowsAffected()
	dcmd, err := tx.Exec(ctx,
		`DELETE FROM bank_statement_imports WHERE organisation_id=$1 AND import_id=$2`,
		orgID, importID)
	if err != nil {
		return 0, err
	}
	if dcmd.RowsAffected() == 0 {
		return 0, ErrNotFound
	}
	return deleted, tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// Reconcile periods
// ---------------------------------------------------------------------------

// CreatePeriod locks a reconciled range on one bank account. Overlapping
// periods are rejected: they would make the lock ambiguous.
func (r *BankStatementRepository) CreatePeriod(ctx context.Context, orgID uuid.UUID, p *models.BankReconcilePeriod, createdBy *uuid.UUID) error {
	if err := r.requireAccountInOrg(ctx, r.pool, orgID, p.BankAccountID); err != nil {
		return err
	}
	var overlap int
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM bank_reconcile_periods
		 WHERE organisation_id=$1 AND bank_account_id=$2
		   AND start_date <= $4 AND end_date >= $3`,
		orgID, p.BankAccountID, p.StartDate, p.EndDate).Scan(&overlap); err != nil {
		return err
	}
	if overlap > 0 {
		return ErrAlreadyExists
	}
	return r.pool.QueryRow(ctx,
		`INSERT INTO bank_reconcile_periods
			(organisation_id, bank_account_id, start_date, end_date, statement_balance, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING period_id, created_at`,
		orgID, p.BankAccountID, p.StartDate, p.EndDate, p.StatementBalance, createdBy,
	).Scan(&p.PeriodID, &p.CreatedAt)
}

func (r *BankStatementRepository) ListPeriods(ctx context.Context, orgID uuid.UUID, bankAccountID *uuid.UUID) ([]models.BankReconcilePeriod, error) {
	where := " WHERE organisation_id=$1"
	args := []any{orgID}
	if bankAccountID != nil {
		args = append(args, *bankAccountID)
		where += " AND bank_account_id=$" + strconv.Itoa(len(args))
	}
	rows, err := r.pool.Query(ctx,
		`SELECT period_id, bank_account_id, start_date, end_date, statement_balance, created_at
		 FROM bank_reconcile_periods`+where+` ORDER BY start_date DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BankReconcilePeriod
	for rows.Next() {
		var p models.BankReconcilePeriod
		if err := rows.Scan(&p.PeriodID, &p.BankAccountID, &p.StartDate, &p.EndDate,
			&p.StatementBalance, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *BankStatementRepository) DeletePeriod(ctx context.Context, orgID, periodID uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`DELETE FROM bank_reconcile_periods WHERE organisation_id=$1 AND period_id=$2`,
		orgID, periodID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Reconcile header
// ---------------------------------------------------------------------------

// StatementBalance is the pair of numbers at the top of every Xero reconcile
// screen: "Statement balance" from the bank, and "Balance in Xero" from the
// ledger. The two agree exactly when everything in the inbox has been dealt
// with, which is what makes the difference a useful progress indicator.
type StatementBalance struct {
	BankAccountID     uuid.UUID       `json:"BankAccountID"`
	LedgerBalance     decimal.Decimal `json:"LedgerBalance"`
	StatementBalance  decimal.Decimal `json:"StatementBalance"`
	Difference        decimal.Decimal `json:"Difference"`
	UnreconciledCount int             `json:"UnreconciledCount"`
	ReconciledCount   int             `json:"ReconciledCount"`
	LastStatementEnd  *time.Time      `json:"LastStatementEnd,omitempty"`
	LastImportedAt    *time.Time      `json:"LastImportedAt,omitempty"`
	LastSyncAt        *time.Time      `json:"LastSyncAt,omitempty"`
}

// AccountBalance computes the reconcile header for one bank account.
//
// The ledger balance is the accumulated GL posting on the account. Statement
// lines still in the inbox have not been posted yet, so the bank's own balance
// is the ledger balance plus those unreconciled lines — that is why the two
// columns converge as the user works through the inbox.
func (r *BankStatementRepository) AccountBalance(ctx context.Context, orgID, bankAccountID uuid.UUID) (*StatementBalance, error) {
	sb := &StatementBalance{BankAccountID: bankAccountID}

	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(l.net_amount),0)
		 FROM gl_journal_lines l
		 JOIN gl_journals j ON j.journal_id = l.journal_id
		 WHERE j.organisation_id=$1 AND l.account_id=$2`,
		orgID, bankAccountID).Scan(&sb.LedgerBalance); err != nil {
		return nil, err
	}
	var pending decimal.Decimal
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM bank_statement_lines
		 WHERE organisation_id=$1 AND status='NEW'
		   AND (bank_account_id=$2
		        OR feed_account_id IN (SELECT feed_account_id FROM bank_feed_accounts WHERE account_id=$2))`,
		orgID, bankAccountID).Scan(&pending); err != nil {
		return nil, err
	}
	sb.StatementBalance = sb.LedgerBalance.Add(pending)
	sb.Difference = sb.StatementBalance.Sub(sb.LedgerBalance)

	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE status='NEW'),
		        COUNT(*) FILTER (WHERE status='IMPORTED'),
		        MAX(posted_at) FILTER (WHERE status='NEW'),
		        MAX(imported_at)
		 FROM bank_statement_lines
		 WHERE organisation_id=$1
		   AND (bank_account_id=$2
		        OR feed_account_id IN (SELECT feed_account_id FROM bank_feed_accounts WHERE account_id=$2))`,
		orgID, bankAccountID).Scan(&sb.UnreconciledCount, &sb.ReconciledCount,
		&sb.LastStatementEnd, &sb.LastImportedAt); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT MAX(c.last_synced_at) FROM bank_feed_connections c
		 JOIN bank_feed_accounts a ON a.connection_id = c.connection_id
		 WHERE c.organisation_id=$1 AND a.account_id=$2`,
		orgID, bankAccountID).Scan(&sb.LastSyncAt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return sb, nil
}

// UnreconciledLinesForAccount returns the inbox of one bank account: the lines
// still awaiting a decision, reached either directly (an import) or through the
// feed account bound to it.
func (r *BankStatementRepository) UnreconciledLinesForAccount(ctx context.Context, orgID, bankAccountID uuid.UUID) ([]models.BankStatementLine, error) {
	q := "SELECT " + statementLineColumns + statementLineFrom + `
		WHERE l.organisation_id=$1 AND l.status='NEW'
		  AND (l.bank_account_id=$2
		       OR l.feed_account_id IN (SELECT feed_account_id FROM bank_feed_accounts WHERE account_id=$2))
		ORDER BY l.posted_at DESC`
	rows, err := r.pool.Query(ctx, q, orgID, bankAccountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BankStatementLine
	for rows.Next() {
		s, err := scanStatementLine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}
