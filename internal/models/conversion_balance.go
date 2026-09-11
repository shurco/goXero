package models

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ConversionBalance is the set of opening balances an organisation brings in
// when it converts to goXero, on the screen at
// /app/settings/conversion-balances: the date the balances are stated as at,
// the lock that protects them from accidental edits, and one line per account.
//
// ConversionDate is a plain "YYYY-MM-DD" string rather than a timestamp: a
// conversion date is a calendar date with no time of day, and that is the form
// the screen's date input reads and writes. An empty date means the
// organisation has not converted.
type ConversionBalance struct {
	ConversionDate string                  `json:"ConversionDate"`
	Locked         bool                    `json:"Locked"`
	Lines          []ConversionBalanceLine `json:"Lines"`
}

// ConversionBalanceLine is one account's opening balance.
//
// Amount is signed the way the ledger signs it -- a debit is positive, a credit
// negative -- so the screen's two columns are one figure that cannot disagree
// with itself, and so the line can be posted without a second translation.
// Code is the ledger's own name for the account, carried so a line still names
// its account on a screen whose picker lists only the active chart.
type ConversionBalanceLine struct {
	AccountID uuid.UUID       `json:"AccountID"`
	Code      string          `json:"Code,omitempty"`
	Amount    decimal.Decimal `json:"Amount"`
}
