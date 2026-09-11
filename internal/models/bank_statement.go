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

// StatementImportStaged is the status of an import the user has previewed but
// not yet committed.
const StatementImportStaged = "STAGED"

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
	AccountCode string     `json:"AccountCode,omitempty"`
	AccountID   *uuid.UUID `json:"AccountID,omitempty"`
	TaxType     string     `json:"TaxType,omitempty"`
	// AutoReconciledAt is set only when AutoReconcile itself reconciled the
	// line. It is what lets the banner say how many lines the button dealt
	// with, as opposed to how many happen to be reconciled.
	AutoReconciledAt *time.Time `json:"AutoReconciledAt,omitempty"`
	// Comments is the "Discuss" thread, filled in by the caller that lists them.
	Comments []BankStatementLineComment `json:"Comments,omitempty"`
	// CodedAccountCode and CodedAccountName are read-only: the coding of the
	// transaction this line became, once it has one. Xero shows them as the
	// Code column on the Bank statements tab.
	CodedAccountCode string `json:"CodedAccountCode,omitempty"`
	CodedAccountName string `json:"CodedAccountName,omitempty"`
	// CommentCount is how many notes the line carries. Xero marks the Discuss
	// tab with a " *" whenever there is one, which means the count has to be
	// known before the panel is opened — the thread itself is only read when
	// the tab is.
	CommentCount int                  `json:"CommentCount"`
	Suggestions  []BankRuleSuggestion `json:"Suggestions,omitempty"`
	// PreviousEntry is the "suggest previous entries" coding. Like a bank rule
	// suggestion it is advisory: the Create panel opens with it filled in and
	// anything can be changed before saving.
	PreviousEntry *PreviousEntrySuggestion `json:"PreviousEntry,omitempty"`
}

// PreviousEntrySuggestion is what Xero calls "suggest previous entries": the
// coding this bank account used the last few times a line with the same payee
// went through it. It answers the question a bookkeeper actually asks — "how
// did we code this before?" — so the Create panel can open pre-filled instead
// of blank.
//
// The coding is taken from the most recent previous entry; MatchCount says how
// many were found, so the UI can be honest about how strong the hint is.
type PreviousEntrySuggestion struct {
	Payee       string     `json:"Payee,omitempty"`
	MatchCount  int        `json:"MatchCount"`
	LastUsedAt  *time.Time `json:"LastUsedAt,omitempty"`
	ContactID   *uuid.UUID `json:"ContactID,omitempty"`
	ContactName string     `json:"ContactName,omitempty"`
	AccountCode string     `json:"AccountCode,omitempty"`
	AccountID   *uuid.UUID `json:"AccountID,omitempty"`
	AccountName string     `json:"AccountName,omitempty"`
	TaxType     string     `json:"TaxType,omitempty"`
	Description string     `json:"Description,omitempty"`
	Reference   string     `json:"Reference,omitempty"`
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

// BankStatementLineComment is one note on a statement line — Xero's "Discuss".
// A line in the inbox is often a question ("what is this $42.50?"), and the
// answer belongs next to the line rather than in someone's inbox.
type BankStatementLineComment struct {
	CommentID       uuid.UUID  `json:"CommentID"`
	StatementLineID uuid.UUID  `json:"StatementLineID"`
	UserID          *uuid.UUID `json:"UserID,omitempty"`
	AuthorName      string     `json:"AuthorName,omitempty"`
	Body            string     `json:"Body"`
	CreatedAt       time.Time  `json:"CreatedDateUTC"`
}

// AutoReconcileReport is the number behind Xero's auto-reconcile banner: how
// many of the statement lines that arrived in the window were reconciled by
// the button rather than by hand. It carries the setting too, because the
// banner is where the user turns it on.
type AutoReconcileReport struct {
	Days             int  `json:"Days"`
	Total            int  `json:"Total"`
	AutoReconciled   int  `json:"AutoReconciled"`
	Enabled          bool `json:"Enabled"`
	UnreconciledLeft int  `json:"UnreconciledLeft"`
}
