package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("record not found")
	ErrAlreadyExists = errors.New("record already exists")
	ErrForbidden     = errors.New("forbidden")
	ErrInvalidInput  = errors.New("invalid input")
)

// rowQuerier is satisfied by both *pgxpool.Pool and a pgx.Tx, so a guard such as
// requireAccountInOrg can run either standalone or inside a transaction.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// requireAccountInOrg refuses an account that another tenant owns, so a caller
// cannot reference it — and, through a posting, move money into or out of it —
// by passing its UUID. Returns ErrNotFound so the handler masks it as a 404.
func requireAccountInOrg(ctx context.Context, q rowQuerier, orgID, accountID uuid.UUID) error {
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

// pgErrCodeUniqueViolation is PostgreSQL's SQLSTATE for unique_violation.
// Centralised so repositories don't sprinkle magic strings.
const pgErrCodeUniqueViolation = "23505"

// isUniqueViolation is a convenience wrapper around pgErrCodeUniqueViolation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgErrCodeUniqueViolation
}

// Repositories bundles all available repositories so handlers can receive a
// single dependency.
type Repositories struct {
	Pool               *pgxpool.Pool
	Organisations      *OrganisationRepository
	Users              *UserRepository
	Accounts           *AccountRepository
	TaxRates           *TaxRateRepository
	Currencies         *CurrencyRepository
	Contacts           *ContactRepository
	ContactGroups      *ContactGroupRepository
	Items              *ItemRepository
	Invoices           *InvoiceRepository
	CreditNotes        *CreditNoteRepository
	Payments           *PaymentRepository
	BankTransactions   *BankTransactionRepository
	BankTransfers      *BankTransferRepository
	BankRules          *BankRuleRepository
	ManualJournals     *ManualJournalRepository
	Journals           *JournalRepository
	Quotes             *QuoteRepository
	PurchaseOrders     *PurchaseOrderRepository
	BrandingThemes     *BrandingThemeRepository
	TrackingCategories *TrackingCategoryRepository
	Attachments        *AttachmentRepository
	History            *HistoryRepository
	Reports            *ReportRepository

	Prepayments        *PrepaymentRepository
	Overpayments       *OverpaymentRepository
	RepeatingInvoices  *RepeatingInvoiceRepository
	BatchPayments      *BatchPaymentRepository
	LinkedTransactions *LinkedTransactionRepository
	Employees          *EmployeeRepository
	Receipts           *ReceiptRepository
	ExpenseClaims      *ExpenseClaimRepository
	BankFeeds          *BankFeedRepository
	BankStatements     *BankStatementRepository
	RefreshTokens      *RefreshTokenRepository
}

func New(pool *pgxpool.Pool) *Repositories {
	return &Repositories{
		Pool:               pool,
		Organisations:      &OrganisationRepository{pool: pool},
		Users:              &UserRepository{pool: pool},
		Accounts:           &AccountRepository{pool: pool},
		TaxRates:           &TaxRateRepository{pool: pool},
		Currencies:         &CurrencyRepository{pool: pool},
		Contacts:           &ContactRepository{pool: pool},
		ContactGroups:      &ContactGroupRepository{pool: pool},
		Items:              &ItemRepository{pool: pool},
		Invoices:           &InvoiceRepository{pool: pool},
		CreditNotes:        &CreditNoteRepository{pool: pool},
		Payments:           &PaymentRepository{pool: pool},
		BankTransactions:   &BankTransactionRepository{pool: pool},
		BankTransfers:      &BankTransferRepository{pool: pool},
		BankRules:          &BankRuleRepository{pool: pool},
		ManualJournals:     &ManualJournalRepository{pool: pool},
		Journals:           &JournalRepository{pool: pool},
		Quotes:             &QuoteRepository{pool: pool},
		PurchaseOrders:     &PurchaseOrderRepository{pool: pool},
		BrandingThemes:     &BrandingThemeRepository{pool: pool},
		TrackingCategories: &TrackingCategoryRepository{pool: pool},
		Attachments:        &AttachmentRepository{pool: pool},
		History:            &HistoryRepository{pool: pool},
		Reports:            &ReportRepository{pool: pool},
		Prepayments:        &PrepaymentRepository{pool: pool},
		Overpayments:       &OverpaymentRepository{pool: pool},
		RepeatingInvoices:  &RepeatingInvoiceRepository{pool: pool},
		BatchPayments:      &BatchPaymentRepository{pool: pool},
		LinkedTransactions: &LinkedTransactionRepository{pool: pool},
		Employees:          &EmployeeRepository{pool: pool},
		Receipts:           &ReceiptRepository{pool: pool},
		ExpenseClaims:      &ExpenseClaimRepository{pool: pool},
		BankFeeds:          &BankFeedRepository{pool: pool},
		BankStatements:     &BankStatementRepository{pool: pool},
		RefreshTokens:      &RefreshTokenRepository{pool: pool},
	}
}
