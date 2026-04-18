package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Manual Journal is a user-driven double-entry posting. Lines must balance
// (SUM of signed NetAmount == 0) — the repository enforces this on create.
type ManualJournal struct {
	ManualJournalID        uuid.UUID           `json:"ManualJournalID"`
	Narration              string              `json:"Narration"`
	Date                   *time.Time          `json:"Date,omitempty"`
	LineAmountTypes        string              `json:"LineAmountTypes"`
	Status                 string              `json:"Status"`
	URL                    string              `json:"Url,omitempty"`
	ShowOnCashBasisReports bool                `json:"ShowOnCashBasisReports"`
	JournalLines           []ManualJournalLine `json:"JournalLines,omitempty"`
	UpdatedDateUTC         time.Time           `json:"UpdatedDateUTC"`
}

type ManualJournalLine struct {
	LineID      uuid.UUID       `json:"LineID"`
	Description string          `json:"Description,omitempty"`
	AccountCode string          `json:"AccountCode,omitempty"`
	TaxType     string          `json:"TaxType,omitempty"`
	TaxAmount   decimal.Decimal `json:"TaxAmount"`
	LineAmount  decimal.Decimal `json:"LineAmount"`
}

// GL journal row (read-only view).
type Journal struct {
	JournalID      uuid.UUID     `json:"JournalID"`
	JournalNumber  int64         `json:"JournalNumber"`
	JournalDate    time.Time     `json:"JournalDate"`
	CreatedDateUTC time.Time     `json:"CreatedDateUTC"`
	Reference      string        `json:"Reference,omitempty"`
	SourceID       *uuid.UUID    `json:"SourceID,omitempty"`
	SourceType     string        `json:"SourceType,omitempty"`
	JournalLines   []JournalLine `json:"JournalLines,omitempty"`
}

type JournalLine struct {
	JournalLineID uuid.UUID       `json:"JournalLineID"`
	AccountID     uuid.UUID       `json:"AccountID"`
	AccountCode   string          `json:"AccountCode,omitempty"`
	AccountName   string          `json:"AccountName,omitempty"`
	AccountType   string          `json:"AccountType,omitempty"`
	Description   string          `json:"Description,omitempty"`
	TaxType       string          `json:"TaxType,omitempty"`
	TaxAmount     decimal.Decimal `json:"TaxAmount"`
	NetAmount     decimal.Decimal `json:"NetAmount"`
	GrossAmount   decimal.Decimal `json:"GrossAmount"`
}
