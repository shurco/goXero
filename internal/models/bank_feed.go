package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Bank feed connection lifecycle.
const (
	BankFeedStatusPending = "PENDING" // consent created, awaiting user redirect
	BankFeedStatusLinked  = "LINKED"  // consent granted, accounts discoverable
	BankFeedStatusError   = "ERROR"   // provider rejected / requires relink
	BankFeedStatusRevoked = "REVOKED" // user revoked or expired

	BankFeedLineStatusNew      = "NEW"
	BankFeedLineStatusImported = "IMPORTED"
	BankFeedLineStatusIgnored  = "IGNORED"
)

// BankFeedConnection models a per-tenant Open Banking link created via one of
// the registered providers (`internal/bankfeed`). We never persist raw bank
// credentials; `ExternalReference` is whatever opaque id the provider issues
// during consent (e.g. GoCardless `requisition_id`).
type BankFeedConnection struct {
	ConnectionID      uuid.UUID         `json:"ConnectionID"`
	Provider          string            `json:"Provider"`
	Status            string            `json:"Status"`
	InstitutionID     string            `json:"InstitutionID,omitempty"`
	InstitutionName   string            `json:"InstitutionName,omitempty"`
	ExternalReference string            `json:"ExternalReference,omitempty"`
	AuthURL           string            `json:"AuthURL,omitempty"`
	LastError         string            `json:"LastError,omitempty"`
	LastSyncedAt      *time.Time        `json:"LastSyncedAt,omitempty"`
	CreatedAt         time.Time         `json:"CreatedAt"`
	UpdatedAt         time.Time         `json:"UpdatedAt"`
	Accounts          []BankFeedAccount `json:"Accounts,omitempty"`
}

// BankFeedAccount links an upstream bank account to our ledger account.
// `AccountID` is nullable until a user binds the feed to a `BANK` account.
type BankFeedAccount struct {
	FeedAccountID     uuid.UUID        `json:"FeedAccountID"`
	ConnectionID      uuid.UUID        `json:"ConnectionID"`
	AccountID         *uuid.UUID       `json:"AccountID,omitempty"`
	ExternalAccountID string           `json:"ExternalAccountID"`
	DisplayName       string           `json:"DisplayName,omitempty"`
	IBAN              string           `json:"IBAN,omitempty"`
	CurrencyCode      string           `json:"CurrencyCode,omitempty"`
	Balance           *decimal.Decimal `json:"Balance,omitempty"`
	UpdatedAt         time.Time        `json:"UpdatedAt"`
}

// BankFeedStatementLine is a raw row pulled from the provider, staged here so
// the user can review before it becomes a `BankTransaction`. Uniqueness on
// (FeedAccountID, ProviderTxID) makes re-syncing idempotent.
type BankFeedStatementLine struct {
	StatementLineID   uuid.UUID       `json:"StatementLineID"`
	FeedAccountID     uuid.UUID       `json:"FeedAccountID"`
	ProviderTxID      string          `json:"ProviderTxID"`
	PostedAt          time.Time       `json:"PostedAt"`
	Amount            decimal.Decimal `json:"Amount"`
	CurrencyCode      string          `json:"CurrencyCode"`
	Description       string          `json:"Description,omitempty"`
	Counterparty      string          `json:"Counterparty,omitempty"`
	Reference         string          `json:"Reference,omitempty"`
	Status            string          `json:"Status"`
	BankTransactionID *uuid.UUID      `json:"BankTransactionID,omitempty"`
	ImportedAt        *time.Time      `json:"ImportedAt,omitempty"`
	CreatedAt         time.Time       `json:"CreatedAt"`
}
