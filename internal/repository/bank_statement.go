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

	"github.com/shurco/goxero/internal/bankcoding"
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

// statementLineAccountFor is the SQL for the ledger account a line belongs to,
// under the given table alias. A feed line carries no bank_account_id of its
// own — it hangs off a feed account, which is bound to a ledger account
// separately, and often after the lines arrived — so every read path has to
// resolve it the same way, the running balance below included.
func statementLineAccountFor(alias string) string {
	return `COALESCE(` + alias + `.bank_account_id,
	         (SELECT fa.account_id FROM bank_feed_accounts fa
	          WHERE fa.feed_account_id = ` + alias + `.feed_account_id))`
}

// statementLineColumns is the canonical projection for a statement line. It is
// shared by every read path so a new column only has to be added once.
//
// The balance is Xero's running balance rather than the bank's raw figure: the
// bank's own number when the statement printed one, otherwise the account's
// running total at this line, computed in statementLineFrom. A reader of one
// line therefore sees the same number whichever page it was fetched on.
var statementLineColumns = `
	l.statement_line_id, l.feed_account_id,
	` + statementLineAccountFor("l") + ` AS bank_account_id,
	l.import_id, l.source,
	COALESCE(l.provider_tx_id,''), l.posted_at, l.amount,
	COALESCE(l.balance, lb.running_balance), COALESCE(l.currency_code,''),
	COALESCE(l.payee,''), COALESCE(l.description,''), COALESCE(l.counterparty,''),
	COALESCE(l.reference,''), COALESCE(l.cheque_number,''), l.status, l.bank_transaction_id,
	l.coded_at, l.coded_by, l.imported_at, l.created_at, l.auto_reconciled_at,
	COALESCE(cli.account_code,''), COALESCE(ca.name,''),
	(SELECT COUNT(*) FROM bank_statement_line_comments bslc
	  WHERE bslc.statement_line_id = l.statement_line_id)`

// statementLineFrom reaches the coding of the transaction a line became and
// gives the line its running balance.
//
// The balance window runs over the account's lines up to and including this
// one, ordered by posted_at then statement_line_id, and is anchored on the
// opening balance the account's earliest statement recorded. Running it here,
// over the account, rather than over the page the caller asked for is the whole
// point: a line's balance must not change because the caller paged.
//
// The opening balance comes from bank_statement_imports rather than from the
// first line because the first line only knows the balance *after* itself, and
// only when the file carried one at all; when no statement recorded an opening
// balance the running total starts from the account's first line, which is what
// the reconcile screen showed before there was a server-side number.
//
// The coding join is the line's own Code column on Xero's Bank statements tab,
// and reading it here rather than from the caller's transaction list means it
// survives the paging of that list.
var statementLineFrom = ` FROM bank_statement_lines l
	LEFT JOIN LATERAL (
	    SELECT w.running + COALESCE((
	               SELECT i.opening_balance
	                 FROM bank_statement_imports i
	                WHERE i.organisation_id = l.organisation_id
	                  AND i.bank_account_id = ` + statementLineAccountFor("l") + `
	                  AND i.opening_balance IS NOT NULL
	                ORDER BY i.statement_start NULLS LAST, i.created_at
	                LIMIT 1), 0) AS running_balance
	      FROM (
	            SELECT p.statement_line_id,
	                   SUM(p.amount) OVER (ORDER BY p.posted_at, p.statement_line_id
	                                       ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS running
	              FROM bank_statement_lines p
	             WHERE p.organisation_id = l.organisation_id
	               AND ` + statementLineAccountFor("p") + ` = ` + statementLineAccountFor("l") + `
	               AND (p.posted_at, p.statement_line_id)
	                   <= (l.posted_at, l.statement_line_id)
	           ) w
	     WHERE w.statement_line_id = l.statement_line_id
	) lb ON TRUE
	LEFT JOIN LATERAL (
	    SELECT li.account_code
	      FROM bank_transaction_line_items li
	     WHERE li.bank_transaction_id = l.bank_transaction_id
	     ORDER BY li.line_item_id
	     LIMIT 1
	) cli ON TRUE
	LEFT JOIN accounts ca ON ca.organisation_id = l.organisation_id AND ca.code = cli.account_code`

func scanStatementLine(row pgx.Row) (*models.BankStatementLine, error) {
	s := &models.BankStatementLine{}
	err := row.Scan(
		&s.StatementLineID, &s.FeedAccountID, &s.BankAccountID, &s.ImportID, &s.Source,
		&s.ProviderTxID, &s.PostedAt, &s.Amount, &s.Balance, &s.CurrencyCode,
		&s.Payee, &s.Description, &s.Counterparty,
		&s.Reference, &s.ChequeNumber, &s.Status, &s.BankTransactionID,
		&s.CodedAt, &s.CodedBy, &s.ImportedAt, &s.CreatedAt, &s.AutoReconciledAt,
		&s.CodedAccountCode, &s.CodedAccountName,
		&s.CommentCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

// codingHistoryLimit bounds how far back "suggest previous entries" looks. A
// payee nobody has coded in the last few hundred entries is not a habit worth
// inferring a code from, and the cap keeps the lookup a single small query.
const codingHistoryLimit = 500

// CodingHistory reads the account's previously coded entries, oldest first.
//
// An entry is any transaction on the account that already carries a coding —
// whether it was created from a reconciled statement line or entered straight
// into Xero — because both are "a previous entry" to the person looking at the
// next line. The payee an entry is remembered by is the bank's own payee text
// when there is one: it is what the next statement line will also say.
func (r *BankStatementRepository) CodingHistory(ctx context.Context, orgID, bankAccountID uuid.UUID, limit int) ([]bankcoding.Entry, error) {
	if limit <= 0 {
		limit = codingHistoryLimit
	}
	const q = `
		SELECT t.bank_transaction_id,
		       COALESCE(NULLIF(btrim(l.payee), ''), c.name, COALESCE(t.reference, '')) AS payee,
		       COALESCE(t.reference, ''),
		       COALESCE(l.posted_at, t.date::timestamptz) AS used_at,
		       t.contact_id, COALESCE(c.name, ''),
		       COALESCE(li.account_code, ''), li.account_id, COALESCE(a.name, ''),
		       COALESCE(li.tax_type, ''), COALESCE(li.description, '')
		  FROM bank_transactions t
		  LEFT JOIN bank_statement_lines l ON l.bank_transaction_id = t.bank_transaction_id
		  LEFT JOIN contacts c ON c.contact_id = t.contact_id
		  LEFT JOIN LATERAL (
		      SELECT li.account_code, li.account_id, li.tax_type, li.description
		        FROM bank_transaction_line_items li
		       WHERE li.bank_transaction_id = t.bank_transaction_id
		       ORDER BY li.line_item_id
		       LIMIT 1
		  ) li ON TRUE
		  -- The account is resolved by code, not by the line item's account_id:
		  -- the code is the key every other part of this feature speaks in, and
		  -- older line items carry a code without the id that goes with it.
		  LEFT JOIN accounts a ON a.organisation_id = t.organisation_id AND a.code = li.account_code
		 WHERE t.organisation_id = $1
		   AND t.bank_account_id = $2
		 ORDER BY used_at DESC, t.created_at DESC
		 LIMIT $3`

	rows, err := r.pool.Query(ctx, q, orgID, bankAccountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Newest first is what the LIMIT needs; the history is handed back oldest
	// first so "the most recent match" is simply the last one appended.
	out := make([]bankcoding.Entry, 0, 64)
	seen := make(map[uuid.UUID]bool, 64)
	for rows.Next() {
		var (
			txID uuid.UUID
			e    bankcoding.Entry
		)
		if err := rows.Scan(&txID, &e.Payee, &e.Reference, &e.UsedAt, &e.ContactID, &e.ContactName,
			&e.AccountCode, &e.AccountID, &e.AccountName, &e.TaxType, &e.Description); err != nil {
			return nil, err
		}
		// Several lines can be matched to one transaction; that is one entry,
		// not several, and counting it twice would overstate the evidence.
		if seen[txID] {
			continue
		}
		seen[txID] = true
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
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
	// MinAmount and MaxAmount are Xero's amount range. They compare the
	// magnitude, because "between 10 and 50" is about how big the movement was
	// and nobody means to exclude the debits by it.
	MinAmount *decimal.Decimal
	MaxAmount *decimal.Decimal
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
			strconv.Itoa(len(args)+1) + "))")
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
		// The amount is compared both ways round so that the number printed on
		// the statement finds the line whether or not the user typed the sign:
		// a debit is stored negative, but nobody searches for "-42.50".
		sb.WriteString(" AND (COALESCE(l.payee,'') ILIKE $" + n +
			" OR COALESCE(l.description,'') ILIKE $" + n +
			" OR COALESCE(l.reference,'') ILIKE $" + n +
			" OR COALESCE(l.counterparty,'') ILIKE $" + n +
			" OR COALESCE(l.cheque_number,'') ILIKE $" + n +
			" OR l.amount::text ILIKE $" + n +
			" OR abs(l.amount)::text ILIKE $" + n + ")")
	}
	if f.From != nil {
		add(" AND l.posted_at>=", *f.From)
	}
	if f.To != nil {
		add(" AND l.posted_at<=", *f.To)
	}
	if f.MinAmount != nil {
		add(" AND abs(l.amount)>=", *f.MinAmount)
	}
	if f.MaxAmount != nil {
		add(" AND abs(l.amount)<=", *f.MaxAmount)
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

	// Counted off the bare table: where() only ever speaks about `l`, and the
	// coding join adds nothing to a count but work.
	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM bank_statement_lines l"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	// `created_at` is the same for every line of an import, so on its own it
	// does not order a statement: two pages of the same account could repeat a
	// line and drop another, and the balance a client reads off a line would
	// depend on which page it happened to arrive on. The line id breaks the tie,
	// the same key the running balance accumulates in.
	q := "SELECT " + statementLineColumns + statementLineFrom + where +
		" ORDER BY posted_at DESC, created_at DESC, l.statement_line_id DESC" +
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

// GetLines loads the named lines in one query. The cash-coding grid sends a page
// of ids at a time, so loading them one by one would be a query per row.
//
// Ids are expected to be unique — they identify grid rows — which is what lets a
// short result mean one of them is not in this organisation.
func (r *BankStatementRepository) GetLines(ctx context.Context, orgID uuid.UUID, lineIDs []uuid.UUID) ([]models.BankStatementLine, error) {
	if len(lineIDs) == 0 {
		return nil, nil
	}
	q := "SELECT " + statementLineColumns + statementLineFrom +
		" WHERE l.organisation_id=$1 AND l.statement_line_id = ANY($2::uuid[])"
	rows, err := r.pool.Query(ctx, q, orgID, lineIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.BankStatementLine, 0, len(lineIDs))
	for rows.Next() {
		s, err := scanStatementLine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(lineIDs) {
		return nil, ErrNotFound
	}
	return out, nil
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
	if err := requireAccountInOrg(ctx, r.pool, orgID, imp.BankAccountID); err != nil {
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
	if err := requireAccountInOrg(ctx, r.pool, orgID, p.BankAccountID); err != nil {
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

// ---------------------------------------------------------------------------
// Balance graph
// ---------------------------------------------------------------------------

// LedgerBalancePoint is one day of the balance graph Xero draws on each bank
// account card: the account's ledger balance — Xero's "Balance in Xero" —
// carried forward to the end of that day.
type LedgerBalancePoint struct {
	Date    string          `json:"Date"`
	Balance decimal.Decimal `json:"Balance"`
}

// BalanceSeriesDays is the width of Xero's balance graph: a month ending today.
const BalanceSeriesDays = 31

// BalanceSeries is the line behind the card's balance graph. It is one point
// per calendar day over the window ending today, holding the previous day's
// balance forward over days nothing posted — that is what makes the graph a
// staircase rather than a scatter of the days that happened to move.
//
// The figure plotted is the ledger balance, not the bank's. Xero's graph ends
// on the card's "Balance in Xero" and not on its "Statement balance", and the
// same account can hold both at once (the demo's 090 reads 7,430.22 in the
// graph against 13,985.32 on the statement), so the graph is drawn from the GL
// and not from the statement lines.
//
// The running total is taken over every journal, not only those inside the
// window. That is what makes the last point equal AccountBalance's
// LedgerBalance by construction — including when a journal is dated after
// today, which would otherwise fall outside the window and go missing.
func (r *BankStatementRepository) BalanceSeries(ctx context.Context, orgID, bankAccountID uuid.UUID, days int) ([]LedgerBalancePoint, error) {
	if days < 2 {
		days = BalanceSeriesDays
	}
	if days > 366 {
		days = 366
	}
	rows, err := r.pool.Query(ctx,
		`WITH posting AS (
		     SELECT j.journal_date AS day, SUM(l.net_amount) AS net
		     FROM gl_journals j
		     JOIN gl_journal_lines l ON l.journal_id = j.journal_id
		     WHERE j.organisation_id = $1 AND l.account_id = $2
		     GROUP BY j.journal_date
		 ),
		 span AS (
		     SELECT generate_series(CURRENT_DATE - ($3::int - 1), CURRENT_DATE,
		                            interval '1 day')::date AS day
		 )
		 SELECT span.day,
		        COALESCE((SELECT SUM(net) FROM posting WHERE posting.day <= span.day), 0)
		 FROM span
		 ORDER BY span.day`, orgID, bankAccountID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]LedgerBalancePoint, 0, days)
	for rows.Next() {
		var day time.Time
		var balance decimal.Decimal
		if err := rows.Scan(&day, &balance); err != nil {
			return nil, err
		}
		out = append(out, LedgerBalancePoint{Date: day.Format("2006-01-02"), Balance: balance})
	}
	return out, rows.Err()
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

// ---------------------------------------------------------------------------
// Discuss — the notes left on a statement line
// ---------------------------------------------------------------------------

// Comments lists a line's discussion oldest first, which is the order a
// conversation is read in.
func (r *BankStatementRepository) Comments(ctx context.Context, orgID, lineID uuid.UUID) ([]models.BankStatementLineComment, error) {
	// The line is looked up first so a comment list for someone else's line is
	// a not-found rather than an empty thread.
	if _, err := r.GetLine(ctx, orgID, lineID); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT comment_id, statement_line_id, user_id, COALESCE(author_name,''), body, created_at
		   FROM bank_statement_line_comments
		  WHERE organisation_id=$1 AND statement_line_id=$2
		  ORDER BY created_at, comment_id`, orgID, lineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.BankStatementLineComment
	for rows.Next() {
		var c models.BankStatementLineComment
		if err := rows.Scan(&c.CommentID, &c.StatementLineID, &c.UserID, &c.AuthorName, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddComment records one note against a statement line.
func (r *BankStatementRepository) AddComment(ctx context.Context, orgID, lineID uuid.UUID, userID *uuid.UUID, authorName, body string) (*models.BankStatementLineComment, error) {
	if _, err := r.GetLine(ctx, orgID, lineID); err != nil {
		return nil, err
	}
	c := &models.BankStatementLineComment{
		StatementLineID: lineID,
		UserID:          userID,
		AuthorName:      authorName,
		Body:            body,
	}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO bank_statement_line_comments
		     (organisation_id, statement_line_id, user_id, author_name, body)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING comment_id, created_at`,
		orgID, lineID, userID, authorName, body).Scan(&c.CommentID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// Options — deleting a line, and the auto-reconcile report
// ---------------------------------------------------------------------------

// DeleteLine removes a statement line the user does not want. A reconciled line
// is refused: it is the evidence behind a ledger entry, and deleting it would
// leave that entry unexplained.
func (r *BankStatementRepository) DeleteLine(ctx context.Context, orgID, lineID uuid.UUID) error {
	cmd, err := r.pool.Exec(ctx,
		`DELETE FROM bank_statement_lines
		  WHERE organisation_id=$1 AND statement_line_id=$2 AND bank_transaction_id IS NULL
		    AND status <> 'IMPORTED'`, orgID, lineID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		// Zero rows means either the line is not there at all or it exists but
		// is protected. The two deserve different answers, so ask which it is.
		if _, err := r.GetLine(ctx, orgID, lineID); err != nil {
			return err
		}
		return ErrForbidden
	}
	return nil
}

// MarkAutoReconciled stamps the lines AutoReconcile dealt with on its own. It
// is separate from AttachLineTransaction so that a hand-made match is never
// counted as the button's work.
func (r *BankStatementRepository) MarkAutoReconciled(ctx context.Context, orgID uuid.UUID, lineIDs []uuid.UUID) error {
	if len(lineIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE bank_statement_lines SET auto_reconciled_at=now()
		  WHERE organisation_id=$1 AND statement_line_id = ANY($2::uuid[])`, orgID, lineIDs)
	return err
}

// AutoReconcileReport counts what the auto-reconcile banner reports: of the
// statement lines that arrived on this account in the last `days`, how many the
// button reconciled by itself, and how many are still waiting.
func (r *BankStatementRepository) AutoReconcileReport(ctx context.Context, orgID, accountID uuid.UUID, days int) (*models.AutoReconcileReport, error) {
	rep := &models.AutoReconcileReport{Days: days}
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*),
		        COUNT(*) FILTER (WHERE l.auto_reconciled_at IS NOT NULL),
		        COUNT(*) FILTER (WHERE l.status = 'NEW'),
		        (SELECT a.auto_reconcile FROM accounts a
		          WHERE a.organisation_id=$1 AND a.account_id=$2)
		   FROM bank_statement_lines l
		  WHERE l.organisation_id=$1
		    AND l.created_at >= now() - make_interval(days => $3)
		    AND (l.bank_account_id=$2
		         OR l.feed_account_id IN (SELECT feed_account_id FROM bank_feed_accounts WHERE account_id=$2))`,
		orgID, accountID, days).Scan(&rep.Total, &rep.AutoReconciled, &rep.UnreconciledLeft, &rep.Enabled)
	if err != nil {
		return nil, err
	}
	return rep, nil
}
