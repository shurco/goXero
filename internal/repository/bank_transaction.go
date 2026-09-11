package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

type BankTransactionRepository struct {
	pool *pgxpool.Pool
}

const bankTxColumns = `
	b.bank_transaction_id, b.type, b.contact_id, COALESCE(c.name,''),
	b.bank_account_id, COALESCE(a.name,''), COALESCE(a.code,''),
	b.is_reconciled, b.date, COALESCE(b.reference,''),
	COALESCE(b.currency_code,''), b.currency_rate, COALESCE(b.url,''),
	b.status, b.line_amount_types,
	b.sub_total, b.total_tax, b.total, b.updated_date_utc`

const bankTxFromJoin = ` FROM bank_transactions b
	LEFT JOIN contacts c ON c.contact_id = b.contact_id
	LEFT JOIN accounts a ON a.account_id = b.bank_account_id`

func scanBankTx(row pgx.Row) (*models.BankTransaction, error) {
	bt := &models.BankTransaction{}
	var (
		contactID *uuid.UUID
		contactNm string
		bankID    *uuid.UUID
		bankNm    string
		bankCode  string
	)
	err := row.Scan(
		&bt.BankTransactionID, &bt.Type, &contactID, &contactNm,
		&bankID, &bankNm, &bankCode,
		&bt.IsReconciled, &bt.Date, &bt.Reference,
		&bt.CurrencyCode, &bt.CurrencyRate, &bt.URL,
		&bt.Status, &bt.LineAmountTypes,
		&bt.SubTotal, &bt.TotalTax, &bt.Total, &bt.UpdatedDateUTC,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	bt.ContactID = contactID
	if contactID != nil && contactNm != "" {
		bt.Contact = &models.Contact{ContactID: *contactID, Name: contactNm}
	}
	bt.BankAccountID = bankID
	if bankID != nil {
		bt.BankAccount = &models.Account{AccountID: *bankID, Name: bankNm, Code: bankCode}
	}
	return bt, nil
}

type BankTransactionFilter struct {
	Type   string
	Status string
	// BankAccountID narrows to one bank account — the account-level reconcile
	// views always want this.
	BankAccountID *uuid.UUID
	// IsReconciled, when set, splits the reconcile inbox (false) from the
	// already-reconciled history (true).
	IsReconciled *bool
	// Search matches the reference or the contact name.
	Search string
	// TypePrefix narrows to a family of transaction types — "SPEND" takes in
	// SPEND, SPEND-OVERPAYMENT and SPEND-PREPAYMENT alike. The reconcile
	// screen uses it for direction, which is what decides whether a candidate
	// can even be the same money as the statement line.
	TypePrefix string
	// Currency narrows to one currency code — the "Show USD items only"
	// filter on the reconcile screen.
	Currency string
	// Total, when set, narrows to transactions whose total has that
	// magnitude. Comparing the magnitude rather than the signed figure is
	// deliberate: bank transactions are stored positive and carry their
	// direction in Type, and a user searching "42.50" means the movement,
	// not the side it happened on.
	Total *decimal.Decimal
	// ExcludeStatuses drops rows carrying any of these statuses. DELETE on a
	// bank transaction only sets status='DELETED', so without this a
	// transaction the user has thrown away stays in the list and, on the
	// reconcile screen, is offered as a match candidate — which Xero never
	// does, and which would also misstate "Showing X - Y of Z".
	ExcludeStatuses []string
}

func (r *BankTransactionRepository) List(ctx context.Context, orgID uuid.UUID, f BankTransactionFilter, p models.Pagination) ([]models.BankTransaction, int, error) {
	var sb strings.Builder
	sb.WriteString(bankTxFromJoin)
	sb.WriteString(" WHERE b.organisation_id=$1")
	args := []any{orgID}
	if f.Type != "" {
		args = append(args, f.Type)
		sb.WriteString(" AND b.type=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		sb.WriteString(" AND b.status=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.BankAccountID != nil {
		args = append(args, *f.BankAccountID)
		sb.WriteString(" AND b.bank_account_id=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.IsReconciled != nil {
		args = append(args, *f.IsReconciled)
		sb.WriteString(" AND b.is_reconciled=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.TypePrefix != "" {
		args = append(args, escapeLikePattern(f.TypePrefix)+"%")
		sb.WriteString(" AND b.type ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Currency != "" {
		args = append(args, f.Currency)
		sb.WriteString(" AND COALESCE(b.currency_code,'')=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if f.Total != nil {
		args = append(args, f.Total.Abs())
		sb.WriteString(" AND b.total=$")
		sb.WriteString(strconv.Itoa(len(args)))
	}
	if len(f.ExcludeStatuses) > 0 {
		args = append(args, f.ExcludeStatuses)
		sb.WriteString(" AND b.status <> ALL($")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(")")
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		sb.WriteString(" AND (COALESCE(b.reference,'') ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(" OR COALESCE(c.name,'') ILIKE $")
		sb.WriteString(strconv.Itoa(len(args)))
		sb.WriteString(")")
	}
	where := sb.String()

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*)"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset())
	q := "SELECT " + bankTxColumns + where +
		" ORDER BY b.date DESC NULLS LAST" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.BankTransaction
	for rows.Next() {
		bt, err := scanBankTx(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *bt)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := r.loadLinesForList(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// loadLinesForList fills in the line items of one page of transactions in a
// single query rather than one per transaction.
//
// The list is what the reconcile screen's Account transactions tab is built
// from, and that tab shows the account each transaction was coded to — a
// transaction arriving without its line items leaves that column blank.
func (r *BankTransactionRepository) loadLinesForList(ctx context.Context, list []models.BankTransaction) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(list))
	at := make(map[uuid.UUID]int, len(list))
	for i := range list {
		ids = append(ids, list[i].BankTransactionID)
		at[list[i].BankTransactionID] = i
	}
	rows, err := r.pool.Query(ctx,
		`SELECT bank_transaction_id, line_item_id, COALESCE(description,''), quantity, unit_amount,
		        COALESCE(account_code,''), COALESCE(tax_type,''), tax_amount, line_amount
		 FROM bank_transaction_line_items
		 WHERE bank_transaction_id = ANY($1)
		 ORDER BY bank_transaction_id, line_item_id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var li models.LineItem
		if err := rows.Scan(&id, &li.LineItemID, &li.Description, &li.Quantity, &li.UnitAmount,
			&li.AccountCode, &li.TaxType, &li.TaxAmount, &li.LineAmount); err != nil {
			return err
		}
		i := at[id]
		list[i].LineItems = append(list[i].LineItems, li)
	}
	return rows.Err()
}

func (r *BankTransactionRepository) GetByID(ctx context.Context, orgID, id uuid.UUID) (*models.BankTransaction, error) {
	q := "SELECT " + bankTxColumns + bankTxFromJoin +
		" WHERE b.organisation_id=$1 AND b.bank_transaction_id=$2"
	bt, err := scanBankTx(r.pool.QueryRow(ctx, q, orgID, id))
	if err != nil {
		return nil, err
	}
	if err := r.loadLines(ctx, bt); err != nil {
		return nil, err
	}
	return bt, nil
}

func (r *BankTransactionRepository) loadLines(ctx context.Context, bt *models.BankTransaction) error {
	rows, err := r.pool.Query(ctx,
		`SELECT line_item_id, COALESCE(description,''), quantity, unit_amount,
			COALESCE(account_code,''), COALESCE(tax_type,''), tax_amount, line_amount
		 FROM bank_transaction_line_items
		 WHERE bank_transaction_id=$1 ORDER BY line_item_id`,
		bt.BankTransactionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var li models.LineItem
		if err := rows.Scan(&li.LineItemID, &li.Description, &li.Quantity, &li.UnitAmount,
			&li.AccountCode, &li.TaxType, &li.TaxAmount, &li.LineAmount); err != nil {
			return err
		}
		bt.LineItems = append(bt.LineItems, li)
	}
	return rows.Err()
}

func (r *BankTransactionRepository) Create(ctx context.Context, orgID uuid.UUID, bt *models.BankTransaction) error {
	if err := resolveLineTaxes(ctx, r.pool, orgID, bt); err != nil {
		return err
	}
	recalculateBankTx(bt)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// A bank account on another tenant's book would post this transaction's GL
	// journal against their ledger, so the caller may only name its own.
	if bt.BankAccountID != nil {
		if err := requireAccountInOrg(ctx, tx, orgID, *bt.BankAccountID); err != nil {
			return err
		}
	}

	if err := tx.QueryRow(ctx, `INSERT INTO bank_transactions (
		organisation_id, contact_id, bank_account_id, type,
		is_reconciled, date, reference, currency_code, currency_rate, url,
		status, line_amount_types, sub_total, total_tax, total
	) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),$9,NULLIF($10,''),
		$11,$12,$13,$14,$15)
	  RETURNING bank_transaction_id, updated_date_utc`,
		orgID, bt.ContactID, bt.BankAccountID, bt.Type,
		bt.IsReconciled, bt.Date, bt.Reference, bt.CurrencyCode, bt.CurrencyRate, bt.URL,
		bt.Status, bt.LineAmountTypes, bt.SubTotal, bt.TotalTax, bt.Total,
	).Scan(&bt.BankTransactionID, &bt.UpdatedDateUTC); err != nil {
		return err
	}
	for _, li := range bt.LineItems {
		if _, err := tx.Exec(ctx,
			`INSERT INTO bank_transaction_line_items (
				bank_transaction_id, description, quantity, unit_amount,
				account_code, tax_type, tax_amount, line_amount)
			 VALUES ($1, NULLIF($2,''), $3,$4, NULLIF($5,''), NULLIF($6,''), $7,$8)`,
			bt.BankTransactionID, li.Description, li.Quantity, li.UnitAmount,
			li.AccountCode, li.TaxType, li.TaxAmount, li.LineAmount); err != nil {
			return err
		}
	}
	if bt.Status != "DELETED" {
		if err := postBankTransactionJournal(ctx, tx, orgID, bt); err != nil {
			return fmt.Errorf("post gl journal: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (r *BankTransactionRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	cmd, err := tx.Exec(ctx,
		`UPDATE bank_transactions SET status='DELETED', updated_date_utc=now()
		 WHERE organisation_id=$1 AND bank_transaction_id=$2 AND status <> 'DELETED'`,
		orgID, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM gl_journals WHERE organisation_id=$1 AND source_type='BANKTRANSACTION' AND source_id=$2`,
		orgID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func recalculateBankTx(bt *models.BankTransaction) {
	sub := decimal.Zero
	tax := decimal.Zero
	for i := range bt.LineItems {
		li := &bt.LineItems[i]
		if li.Quantity.IsZero() {
			li.Quantity = decimal.NewFromInt(1)
		}
		li.LineAmount = li.Quantity.Mul(li.UnitAmount)
		sub = sub.Add(li.LineAmount)
		tax = tax.Add(li.TaxAmount)
	}
	bt.SubTotal = sub
	bt.TotalTax = tax
	switch bt.LineAmountTypes {
	case models.LineAmountTypesInclusive:
		// The lines' own amounts already contain their tax, so the total is
		// what the statement says and the subtotal is that total less the tax.
		bt.Total = sub
		bt.SubTotal = sub.Sub(tax)
	case models.LineAmountTypesNoTax:
		bt.Total = sub
		bt.TotalTax = decimal.Zero
	default:
		bt.Total = sub.Add(tax)
	}
}

// moneyPlaces is the number of decimal places money is held to: the scale of
// every amount the capture carries, and the scale Xero rounds a line's tax to.
const moneyPlaces = 2

// isNoTaxType reports whether a line names no rate at all. "NONE" is the
// capture's own marker for a line that is not taxed; the empty string is a line
// that never named one.
func isNoTaxType(taxType string) bool {
	switch strings.ToUpper(strings.TrimSpace(taxType)) {
	case "", "NONE":
		return true
	}
	return false
}

// resolveLineTaxes fills in the tax amount of every line that names a rate but
// carries no amount of its own.
//
// Xero derives a line's tax from its rate, and the reconcile screen sends the
// rate alone, so without this a coded line reaches the ledger naming a rate and
// holding a tax of 0.00 — a taxed line whose tax nothing records. A caller that
// states an amount keeps it: the figure a document was imported with is
// evidence, and this only fills a gap.
//
// A rate this organisation does not have is left alone rather than guessed at.
func resolveLineTaxes(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, bt *models.BankTransaction) error {
	needsRate := false
	for _, li := range bt.LineItems {
		if li.TaxAmount.IsZero() && !isNoTaxType(li.TaxType) {
			needsRate = true
			break
		}
	}
	if !needsRate {
		return nil
	}
	rows, err := pool.Query(ctx,
		`SELECT tax_type, effective_rate FROM tax_rates
		 WHERE organisation_id=$1 ORDER BY tax_type, name`, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	rates := map[string]decimal.Decimal{}
	for rows.Next() {
		var taxType string
		var rate decimal.Decimal
		if err := rows.Scan(&taxType, &rate); err != nil {
			return err
		}
		rates[strings.ToUpper(strings.TrimSpace(taxType))] = rate
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range bt.LineItems {
		li := &bt.LineItems[i]
		if !li.TaxAmount.IsZero() || isNoTaxType(li.TaxType) {
			continue
		}
		rate, ok := rates[strings.ToUpper(strings.TrimSpace(li.TaxType))]
		if !ok {
			continue
		}
		if li.Quantity.IsZero() {
			li.Quantity = decimal.NewFromInt(1)
		}
		li.TaxAmount = lineTax(bt.LineAmountTypes, li.Quantity.Mul(li.UnitAmount), rate)
	}
	return nil
}

// lineTax is the tax a line owes on top of its own amount — or, for a line
// whose amount already contains the tax, the part of that amount which is tax.
//
// Both are rounded to money's own scale, and in the inclusive case the
// subtraction is done from the amount the line states so that the net and the
// tax always come back to it exactly, however the division rounds.
func lineTax(lineAmountTypes string, lineAmount, rate decimal.Decimal) decimal.Decimal {
	if rate.IsZero() {
		return decimal.Zero
	}
	hundred := decimal.NewFromInt(100)
	if lineAmountTypes == models.LineAmountTypesInclusive {
		net := lineAmount.Mul(hundred).DivRound(hundred.Add(rate), moneyPlaces)
		return lineAmount.Sub(net)
	}
	return lineAmount.Mul(rate).DivRound(hundred, moneyPlaces)
}

// Update replaces a bank transaction in place: header fields, the whole set of
// line items, and the GL journal it posted. Xero's `POST /BankTransactions/:id`
// behaves the same way, and it is the endpoint the reconcile screen must call
// when a user approves a transaction — creating a second transaction instead
// (as the UI used to) double-books the money.
func (r *BankTransactionRepository) Update(ctx context.Context, orgID uuid.UUID, bt *models.BankTransaction) error {
	if err := resolveLineTaxes(ctx, r.pool, orgID, bt); err != nil {
		return err
	}
	recalculateBankTx(bt)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	cmd, err := tx.Exec(ctx, `UPDATE bank_transactions SET
			contact_id=$3, bank_account_id=$4, type=$5,
			is_reconciled=$6, date=$7, reference=NULLIF($8,''),
			currency_code=NULLIF($9,''), currency_rate=$10, url=NULLIF($11,''),
			status=$12, line_amount_types=$13,
			sub_total=$14, total_tax=$15, total=$16, updated_date_utc=now()
		 WHERE organisation_id=$1 AND bank_transaction_id=$2`,
		orgID, bt.BankTransactionID, bt.ContactID, bt.BankAccountID, bt.Type,
		bt.IsReconciled, bt.Date, bt.Reference, bt.CurrencyCode, bt.CurrencyRate, bt.URL,
		bt.Status, bt.LineAmountTypes, bt.SubTotal, bt.TotalTax, bt.Total)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM bank_transaction_line_items WHERE bank_transaction_id=$1`,
		bt.BankTransactionID); err != nil {
		return err
	}
	for _, li := range bt.LineItems {
		if _, err := tx.Exec(ctx,
			`INSERT INTO bank_transaction_line_items (
				bank_transaction_id, description, quantity, unit_amount,
				account_code, tax_type, tax_amount, line_amount)
			 VALUES ($1, NULLIF($2,''), $3,$4, NULLIF($5,''), NULLIF($6,''), $7,$8)`,
			bt.BankTransactionID, li.Description, li.Quantity, li.UnitAmount,
			li.AccountCode, li.TaxType, li.TaxAmount, li.LineAmount); err != nil {
			return err
		}
	}
	// Re-post: the amounts, the account or the tax may all have changed, so the
	// old journal is replaced rather than patched.
	if _, err := tx.Exec(ctx,
		`DELETE FROM gl_journals WHERE organisation_id=$1 AND source_type='BANKTRANSACTION' AND source_id=$2`,
		orgID, bt.BankTransactionID); err != nil {
		return err
	}
	if bt.Status != "DELETED" {
		if err := postBankTransactionJournal(ctx, tx, orgID, bt); err != nil {
			return fmt.Errorf("post gl journal: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// Reconcile flips the reconciled flag on an existing transaction. Kept separate
// from Update so the reconcile inbox can approve a transaction without the
// caller having to send the whole record back — and so approving can never
// accidentally rewrite the amounts.
func (r *BankTransactionRepository) Reconcile(ctx context.Context, orgID, id uuid.UUID, reconciled bool) error {
	cmd, err := r.pool.Exec(ctx,
		`UPDATE bank_transactions SET is_reconciled=$3, updated_date_utc=now()
		 WHERE organisation_id=$1 AND bank_transaction_id=$2 AND status <> 'DELETED'`,
		orgID, id, reconciled)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
