package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Statement line sources. Xero shows this as the `Source` column in the
// reconcile inbox: lines pushed by an Open Banking feed versus lines the user
// imported from a file.
const (
	StatementLineSourceFeed   = "FEED"
	StatementLineSourceImport = "IMPORT"
)

// BankStatementImport is one manual statement upload. The parsed rows live in
// `Payload` between the upload and the commit, so the wizard's mapping step can
// re-interpret the file without the user uploading it again.
type BankStatementImport struct {
	ImportID       uuid.UUID        `json:"ImportID"`
	BankAccountID  uuid.UUID        `json:"BankAccountID"`
	BankAccount    *Account         `json:"BankAccount,omitempty"`
	Filename       string           `json:"Filename,omitempty"`
	Format         string           `json:"Format"`
	Status         string           `json:"Status"`
	LineCount      int              `json:"LineCount"`
	ImportedCount  int              `json:"ImportedCount"`
	DuplicateCount int              `json:"DuplicateCount"`
	CurrencyCode   string           `json:"CurrencyCode,omitempty"`
	StatementStart *time.Time       `json:"StatementStart,omitempty"`
	StatementEnd   *time.Time       `json:"StatementEnd,omitempty"`
	OpeningBalance *decimal.Decimal `json:"OpeningBalance,omitempty"`
	ClosingBalance *decimal.Decimal `json:"ClosingBalance,omitempty"`
	// Mapping is the column mapping the user confirmed in step 2 of the wizard.
	Mapping     map[string]any `json:"Mapping,omitempty"`
	LastError   string         `json:"LastError,omitempty"`
	CreatedAt   time.Time      `json:"CreatedDateUTC"`
	CommittedAt *time.Time     `json:"CommittedAt,omitempty"`
}

// BankStatementImport status values.
const (
	StatementImportStaged   = "STAGED"
	StatementImportImported = "IMPORTED"
)

// BankStatementLine is a single line waiting in the reconcile inbox. A line
// comes from an Open Banking feed (`FeedAccountID` set, Source FEED) or from a
// manual import (`BankAccountID` + `ImportID` set, Source IMPORT); Xero treats
// the two identically once they are here.
type BankStatementLine struct {
	StatementLineID   uuid.UUID        `json:"StatementLineID"`
	FeedAccountID     *uuid.UUID       `json:"FeedAccountID,omitempty"`
	BankAccountID     *uuid.UUID       `json:"BankAccountID,omitempty"`
	ImportID          *uuid.UUID       `json:"ImportID,omitempty"`
	Source            string           `json:"Source"`
	ProviderTxID      string           `json:"ProviderTxID,omitempty"`
	PostedAt          time.Time        `json:"PostedAt"`
	Amount            decimal.Decimal  `json:"Amount"`
	Balance           *decimal.Decimal `json:"Balance,omitempty"`
	CurrencyCode      string           `json:"CurrencyCode"`
	Payee             string           `json:"Payee,omitempty"`
	Description       string           `json:"Description,omitempty"`
	Counterparty      string           `json:"Counterparty,omitempty"`
	Reference         string           `json:"Reference,omitempty"`
	ChequeNumber      string           `json:"ChequeNumber,omitempty"`
	Status            string           `json:"Status"`
	BankTransactionID *uuid.UUID       `json:"BankTransactionID,omitempty"`
	CodedAt           *time.Time       `json:"CodedAt,omitempty"`
	CodedBy           *uuid.UUID       `json:"CodedBy,omitempty"`
	ImportedAt        *time.Time       `json:"ImportedAt,omitempty"`
	CreatedAt         time.Time        `json:"CreatedDateUTC"`

	// Cash coding is filled in by the caller rather than stored on the line:
	// Xero only writes these when the user saves, at which point the line is
	// converted into a BANKTRANSACTION.
	AccountCode string               `json:"AccountCode,omitempty"`
	AccountID   *uuid.UUID           `json:"AccountID,omitempty"`
	TaxType     string               `json:"TaxType,omitempty"`
	Suggestions []BankRuleSuggestion `json:"Suggestions,omitempty"`
}

// BankReconcilePeriod locks a reconciled date range on one bank account so
// only authorised users can change data inside it.
type BankReconcilePeriod struct {
	PeriodID         uuid.UUID       `json:"PeriodID"`
	BankAccountID    uuid.UUID       `json:"BankAccountID"`
	StartDate        time.Time       `json:"StartDate"`
	EndDate          time.Time       `json:"EndDate"`
	StatementBalance decimal.Decimal `json:"StatementBalance"`
	CreatedAt        time.Time       `json:"CreatedDateUTC"`
}

// BankRuleSuggestion is one rule-match result shown in the reconcile inbox.
// It is advisory: the UI offers it as the default account/tax code and the
// user can override before saving.
type BankRuleSuggestion struct {
	BankRuleID uuid.UUID `json:"BankRuleID"`
	RuleName   string    `json:"RuleName"`
	AccountID  string    `json:"AccountID,omitempty"`
	TaxType    string    `json:"TaxType,omitempty"`
	ContactID  string    `json:"ContactID,omitempty"`
}
