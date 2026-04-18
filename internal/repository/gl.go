package repository

// General Ledger — every business event (invoice approval, payment,
// bank transaction, manual journal) is materialised into double-entry
// rows in `gl_journals`/`gl_journal_lines`. Trial Balance / Profit & Loss /
// Balance Sheet / Aged reports all read from these tables.
//
// Sign convention (matches Xero's Journals API):
//
//	net_amount > 0 → debit to the account
//	net_amount < 0 → credit to the account
//
// A well-formed journal MUST sum to zero.
//
// Accounts are resolved by `code` inside the invoice/bank-tx line items
// (Xero's model). A couple of control accounts are referenced by their
// SystemAccount tag (`DEBTORS`, `CREDITORS`). The fallback contra account is
// whichever account has SystemAccount = DEBTORS / CREDITORS for sales/bills.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/shurco/goxero/internal/models"
)

func debtorsAccountID(ctx context.Context, q pgx.Tx, orgID uuid.UUID) (uuid.UUID, error) {
	return systemAccountID(ctx, q, orgID, "DEBTORS")
}

func creditorsAccountID(ctx context.Context, q pgx.Tx, orgID uuid.UUID) (uuid.UUID, error) {
	return systemAccountID(ctx, q, orgID, "CREDITORS")
}

// systemAccountID returns the account_id marked as the given system role
// (DEBTORS for Accounts Receivable, CREDITORS for Accounts Payable).
// If no account is tagged, falls back to account code 610/800 (Xero defaults).
func systemAccountID(ctx context.Context, q pgx.Tx, orgID uuid.UUID, system string) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx,
		`SELECT account_id FROM accounts
		 WHERE organisation_id=$1 AND system_account=$2
		 LIMIT 1`, orgID, system).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	fallback := map[string]string{"DEBTORS": "610", "CREDITORS": "800"}[system]
	if fallback == "" {
		return uuid.Nil, fmt.Errorf("no account tagged %s", system)
	}
	if err := q.QueryRow(ctx,
		`SELECT account_id FROM accounts
		 WHERE organisation_id=$1 AND code=$2 LIMIT 1`, orgID, fallback).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("missing control account %s: %w", system, err)
	}
	return id, nil
}

func accountIDByCode(ctx context.Context, q pgx.Tx, orgID uuid.UUID, code string) (uuid.UUID, error) {
	if code == "" {
		return uuid.Nil, fmt.Errorf("line item is missing AccountCode")
	}
	var id uuid.UUID
	if err := q.QueryRow(ctx,
		`SELECT account_id FROM accounts
		 WHERE organisation_id=$1 AND code=$2 LIMIT 1`, orgID, code).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("unknown account code %q: %w", code, err)
	}
	return id, nil
}

type journalLineInput struct {
	AccountID   uuid.UUID
	Description string
	TaxType     string
	TaxAmount   decimal.Decimal
	NetAmount   decimal.Decimal
}

func insertJournal(
	ctx context.Context, tx pgx.Tx,
	orgID uuid.UUID,
	sourceType string, sourceID uuid.UUID,
	date time.Time, reference string,
	lines []journalLineInput,
) error {
	if len(lines) == 0 {
		return nil
	}
	sum := decimal.Zero
	for _, l := range lines {
		sum = sum.Add(l.NetAmount)
	}
	if !sum.IsZero() {
		return fmt.Errorf("unbalanced journal: sum=%s", sum.String())
	}

	var journalID uuid.UUID
	if err := tx.QueryRow(ctx,
		`INSERT INTO gl_journals
		    (organisation_id, reference, source_type, source_id, journal_date)
		 VALUES ($1, NULLIF($2,''), $3, $4, $5)
		 RETURNING journal_id`,
		orgID, reference, sourceType, sourceID, date).Scan(&journalID); err != nil {
		return err
	}
	for _, l := range lines {
		if _, err := tx.Exec(ctx,
			`INSERT INTO gl_journal_lines
			  (journal_id, account_id, description, tax_type, tax_amount, net_amount, gross_amount)
			 VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,$7)`,
			journalID, l.AccountID, l.Description, l.TaxType,
			l.TaxAmount, l.NetAmount, l.NetAmount.Add(signedTaxFromNet(l.NetAmount, l.TaxAmount)),
		); err != nil {
			return err
		}
	}
	return nil
}

// signedTaxFromNet keeps tax sign aligned with the net amount so gross = net+tax
// stays linear regardless of credit/debit.
func signedTaxFromNet(net, tax decimal.Decimal) decimal.Decimal {
	if net.IsNegative() {
		return tax.Neg()
	}
	return tax
}

// postInvoiceJournal posts a Sales invoice (ACCREC) or a Bill (ACCPAY).
//
// ACCREC example (sale of 100 + 10 GST):
//
//	DR Accounts Receivable  110
//	     CR Revenue             100
//	     CR GST                  10
//
// ACCPAY flips the signs.
func postInvoiceJournal(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, inv *models.Invoice) error {
	if inv.Total.IsZero() {
		return nil
	}
	date := time.Now().UTC()
	if inv.Date != nil {
		date = *inv.Date
	}

	var (
		controlAccID uuid.UUID
		controlSign  = decimal.NewFromInt(1)
		err          error
	)
	switch inv.Type {
	case models.InvoiceTypeAccRec:
		controlAccID, err = debtorsAccountID(ctx, tx, orgID)
	case models.InvoiceTypeAccPay:
		controlAccID, err = creditorsAccountID(ctx, tx, orgID)
		controlSign = decimal.NewFromInt(-1) // bill → credit A/P, debit expenses
	default:
		return fmt.Errorf("unsupported invoice type %q", inv.Type)
	}
	if err != nil {
		return err
	}

	// Resolve the tax control account up-front; if it doesn't exist we fold
	// tax back into each revenue/expense line so the journal still balances.
	taxTotal := decimal.Zero
	for _, li := range inv.LineItems {
		taxTotal = taxTotal.Add(li.TaxAmount)
	}
	taxAccID, taxAccOK := uuid.Nil, false
	if !taxTotal.IsZero() {
		if id, err := accountIDByCode(ctx, tx, orgID, "820"); err == nil {
			taxAccID, taxAccOK = id, true
		}
	}

	lines := make([]journalLineInput, 0, len(inv.LineItems)+2)
	lines = append(lines, journalLineInput{
		AccountID:   controlAccID,
		Description: inv.InvoiceNumber,
		NetAmount:   inv.Total.Mul(controlSign),
	})
	for _, li := range inv.LineItems {
		accID, err := accountIDByCode(ctx, tx, orgID, li.AccountCode)
		if err != nil {
			return err
		}
		gross := li.LineAmount
		if !taxAccOK {
			gross = gross.Add(li.TaxAmount)
		}
		lines = append(lines, journalLineInput{
			AccountID:   accID,
			Description: li.Description,
			TaxType:     li.TaxType,
			TaxAmount:   li.TaxAmount,
			NetAmount:   gross.Mul(controlSign.Neg()),
		})
	}
	if taxAccOK {
		lines = append(lines, journalLineInput{
			AccountID: taxAccID,
			NetAmount: taxTotal.Mul(controlSign.Neg()),
		})
	}
	return insertJournal(ctx, tx, orgID, "INVOICE", inv.InvoiceID, date, inv.InvoiceNumber, lines)
}

// postPaymentJournal moves funds from/to the bank.
//
// Sales payment:  DR Bank ;  CR Accounts Receivable.
// Bill payment :  DR Accounts Payable ; CR Bank.
func postPaymentJournal(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, p *models.Payment) error {
	if p.InvoiceID == nil || p.AccountID == nil {
		return nil
	}
	var invType string
	err := tx.QueryRow(ctx,
		`SELECT type FROM invoices WHERE organisation_id=$1 AND invoice_id=$2`,
		orgID, *p.InvoiceID).Scan(&invType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	var control uuid.UUID
	controlSign := decimal.NewFromInt(1)
	switch invType {
	case models.InvoiceTypeAccRec:
		control, err = debtorsAccountID(ctx, tx, orgID)
		controlSign = decimal.NewFromInt(-1)
	case models.InvoiceTypeAccPay:
		control, err = creditorsAccountID(ctx, tx, orgID)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	bankSign := controlSign.Neg()
	lines := []journalLineInput{
		{AccountID: *p.AccountID, Description: "Payment " + p.Reference, NetAmount: p.Amount.Mul(bankSign)},
		{AccountID: control, Description: "Payment " + p.Reference, NetAmount: p.Amount.Mul(controlSign)},
	}
	return insertJournal(ctx, tx, orgID, "PAYMENT", p.PaymentID, p.Date, p.Reference, lines)
}

// postPaymentReversal is the mirror of postPaymentJournal used when a payment
// is voided: simply strip the journal + lines (FK cascades clear the lines).
func postPaymentReversal(ctx context.Context, tx pgx.Tx, orgID, paymentID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM gl_journals
		 WHERE organisation_id=$1 AND source_type='PAYMENT' AND source_id=$2`,
		orgID, paymentID)
	return err
}

// postBankTransactionJournal posts spend / receive / overpayment bank tx.
func postBankTransactionJournal(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, bt *models.BankTransaction) error {
	if bt.BankAccountID == nil || bt.Total.IsZero() {
		return nil
	}
	date := time.Now().UTC()
	if bt.Date != nil {
		date = *bt.Date
	}
	bankSign := decimal.NewFromInt(1)
	if bt.Type == models.BankTransactionTypeSpend ||
		bt.Type == models.BankTransactionTypeSpendOverpmt ||
		bt.Type == models.BankTransactionTypeSpendPrepmt {
		bankSign = decimal.NewFromInt(-1)
	}
	taxTotal := decimal.Zero
	for _, li := range bt.LineItems {
		taxTotal = taxTotal.Add(li.TaxAmount)
	}
	taxAccID, taxAccOK := uuid.Nil, false
	if !taxTotal.IsZero() {
		if id, err := accountIDByCode(ctx, tx, orgID, "820"); err == nil {
			taxAccID, taxAccOK = id, true
		}
	}

	lines := make([]journalLineInput, 0, len(bt.LineItems)+2)
	lines = append(lines, journalLineInput{
		AccountID: *bt.BankAccountID,
		NetAmount: bt.Total.Mul(bankSign),
	})
	for _, li := range bt.LineItems {
		accID, err := accountIDByCode(ctx, tx, orgID, li.AccountCode)
		if err != nil {
			return err
		}
		gross := li.LineAmount
		if !taxAccOK {
			gross = gross.Add(li.TaxAmount)
		}
		lines = append(lines, journalLineInput{
			AccountID:   accID,
			Description: li.Description,
			TaxType:     li.TaxType,
			TaxAmount:   li.TaxAmount,
			NetAmount:   gross.Mul(bankSign.Neg()),
		})
	}
	if taxAccOK {
		lines = append(lines, journalLineInput{
			AccountID: taxAccID,
			NetAmount: taxTotal.Mul(bankSign.Neg()),
		})
	}
	return insertJournal(ctx, tx, orgID, "BANKTRANSACTION", bt.BankTransactionID, date, bt.Reference, lines)
}

// postManualJournal persists a user-provided double-entry posting. Lines
// must balance; the caller validates this before calling.
func postManualJournal(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, mj *models.ManualJournal) error {
	date := time.Now().UTC()
	if mj.Date != nil {
		date = *mj.Date
	}
	lines := make([]journalLineInput, 0, len(mj.JournalLines))
	for _, l := range mj.JournalLines {
		id, err := accountIDByCode(ctx, tx, orgID, l.AccountCode)
		if err != nil {
			return err
		}
		lines = append(lines, journalLineInput{
			AccountID:   id,
			Description: l.Description,
			TaxType:     l.TaxType,
			TaxAmount:   l.TaxAmount,
			NetAmount:   l.LineAmount,
		})
	}
	return insertJournal(ctx, tx, orgID, "MANUALJOURNAL", mj.ManualJournalID, date, mj.Narration, lines)
}

// postBankTransferJournal: DR to-account; CR from-account.
func postBankTransferJournal(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, bt *models.BankTransfer) error {
	lines := []journalLineInput{
		{AccountID: bt.ToBankAccountID, NetAmount: bt.Amount},
		{AccountID: bt.FromBankAccountID, NetAmount: bt.Amount.Neg()},
	}
	return insertJournal(ctx, tx, orgID, "BANKTRANSFER", bt.BankTransferID, bt.Date, bt.Reference, lines)
}

// postPrepaymentJournal posts a customer prepayment or supplier prepayment.
//
// RECEIVE-PREPAYMENT:  DR Bank ;           CR Accounts Receivable (debit-neg).
// SPEND-PREPAYMENT  :  DR Accounts Payable ; CR Bank.
//
// We use the control account (DEBTORS / CREDITORS) as the offsetting leg so
// the prepayment visibly increases/decreases the contact's balance. This is
// the behaviour Xero describes at
// https://central.xero.com/s/article/Prepayments.
func postPrepaymentJournal(ctx context.Context, tx pgx.Tx, orgID, bankAccID uuid.UUID, p *models.Prepayment) error {
	if p.Total.IsZero() {
		return nil
	}
	date := time.Now().UTC()
	if p.Date != nil {
		date = *p.Date
	}
	var control uuid.UUID
	sign := decimal.NewFromInt(1)
	var err error
	switch p.Type {
	case models.PrepaymentTypeReceive:
		control, err = debtorsAccountID(ctx, tx, orgID)
		sign = decimal.NewFromInt(-1)
	case models.PrepaymentTypeSpend:
		control, err = creditorsAccountID(ctx, tx, orgID)
	default:
		return fmt.Errorf("unsupported prepayment type %q", p.Type)
	}
	if err != nil {
		return err
	}
	lines := []journalLineInput{
		{AccountID: bankAccID, NetAmount: p.Total.Mul(sign.Neg())},
		{AccountID: control, NetAmount: p.Total.Mul(sign)},
	}
	return insertJournal(ctx, tx, orgID, "PREPAYMENT", p.PrepaymentID, date, p.Reference, lines)
}

// postOverpaymentJournal mirrors postPrepaymentJournal for overpayments.
func postOverpaymentJournal(ctx context.Context, tx pgx.Tx, orgID, bankAccID uuid.UUID, o *models.Overpayment) error {
	if o.Total.IsZero() {
		return nil
	}
	date := time.Now().UTC()
	if o.Date != nil {
		date = *o.Date
	}
	var control uuid.UUID
	sign := decimal.NewFromInt(1)
	var err error
	switch o.Type {
	case models.OverpaymentTypeReceive:
		control, err = debtorsAccountID(ctx, tx, orgID)
		sign = decimal.NewFromInt(-1)
	case models.OverpaymentTypeSpend:
		control, err = creditorsAccountID(ctx, tx, orgID)
	default:
		return fmt.Errorf("unsupported overpayment type %q", o.Type)
	}
	if err != nil {
		return err
	}
	lines := []journalLineInput{
		{AccountID: bankAccID, NetAmount: o.Total.Mul(sign.Neg())},
		{AccountID: control, NetAmount: o.Total.Mul(sign)},
	}
	return insertJournal(ctx, tx, orgID, "OVERPAYMENT", o.OverpaymentID, date, o.Reference, lines)
}

// postCreditNoteJournal handles the GL impact of issuing a credit note.
//
// ACCRECCREDIT:  DR Revenue (lines) ;  CR Accounts Receivable (total).
// ACCPAYCREDIT:  DR Accounts Payable (total) ;  CR Expense (lines).
func postCreditNoteJournal(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, cn *models.CreditNote) error {
	if cn.Total.IsZero() {
		return nil
	}
	date := time.Now().UTC()
	if cn.Date != nil {
		date = *cn.Date
	}
	var (
		control uuid.UUID
		sign    decimal.Decimal
		err     error
	)
	switch cn.Type {
	case models.CreditNoteTypeAccRecCredit:
		control, err = debtorsAccountID(ctx, tx, orgID)
		sign = decimal.NewFromInt(-1)
	case models.CreditNoteTypeAccPayCredit:
		control, err = creditorsAccountID(ctx, tx, orgID)
		sign = decimal.NewFromInt(1)
	default:
		return fmt.Errorf("unsupported credit note type %q", cn.Type)
	}
	if err != nil {
		return err
	}

	taxTotal := decimal.Zero
	for _, li := range cn.LineItems {
		taxTotal = taxTotal.Add(li.TaxAmount)
	}
	taxAccID, taxAccOK := uuid.Nil, false
	if !taxTotal.IsZero() {
		if id, err := accountIDByCode(ctx, tx, orgID, "820"); err == nil {
			taxAccID, taxAccOK = id, true
		}
	}

	lines := make([]journalLineInput, 0, len(cn.LineItems)+2)
	lines = append(lines, journalLineInput{
		AccountID: control, Description: cn.CreditNoteNumber,
		NetAmount: cn.Total.Mul(sign),
	})
	for _, li := range cn.LineItems {
		id, err := accountIDByCode(ctx, tx, orgID, li.AccountCode)
		if err != nil {
			return err
		}
		gross := li.LineAmount
		if !taxAccOK {
			gross = gross.Add(li.TaxAmount)
		}
		lines = append(lines, journalLineInput{
			AccountID:   id,
			Description: li.Description,
			TaxType:     li.TaxType,
			TaxAmount:   li.TaxAmount,
			NetAmount:   gross.Mul(sign.Neg()),
		})
	}
	if taxAccOK {
		lines = append(lines, journalLineInput{
			AccountID: taxAccID,
			NetAmount: taxTotal.Mul(sign.Neg()),
		})
	}
	return insertJournal(ctx, tx, orgID, "CREDITNOTE", cn.CreditNoteID, date, cn.CreditNoteNumber, lines)
}
