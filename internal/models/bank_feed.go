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
	Country           string            `json:"Country,omitempty"`
	ExternalReference string            `json:"ExternalReference,omitempty"`
	AuthURL           string            `json:"AuthURL,omitempty"`
	LastError         string            `json:"LastError,omitempty"`
	LastSyncedAt      *time.Time        `json:"LastSyncedAt,omitempty"`
	CreatedAt         time.Time         `json:"CreatedAt"`
	UpdatedAt         time.Time         `json:"UpdatedAt"`
	Accounts          []BankFeedAccount `json:"Accounts,omitempty"`

	// ExternalSecret is the per-connection secret the provider issued during
	// consent (today: a Plaid Item access_token), sealed with AES-256-GCM before
	// it ever reaches the database. It is ciphertext and never leaves the
	// server, so it is excluded from every API response.
	ExternalSecret []byte `json:"-"`
	// SyncCursor is the provider's incremental sync position. It hangs off the
	// connection rather than an account because Plaid cursors track an Item.
	SyncCursor string `json:"-"`
	// SessionRef is the consent session the connection is currently waiting on —
	// Plaid's link token, GoCardless's requisition id. A provider can finish a
	// session out of band (Plaid's SESSION_FINISHED webhook carries nothing but
	// this handle), and `external_reference` cannot stand in for it because
	// consent overwrites that with the durable id.
	SessionRef string `json:"-"`
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
