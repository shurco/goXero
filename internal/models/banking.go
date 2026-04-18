package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	BankTransactionTypeReceive        = "RECEIVE"
	BankTransactionTypeReceiveOverpmt = "RECEIVE-OVERPAYMENT"
	BankTransactionTypeReceivePrepmt  = "RECEIVE-PREPAYMENT"
	BankTransactionTypeSpend          = "SPEND"
	BankTransactionTypeSpendOverpmt   = "SPEND-OVERPAYMENT"
	BankTransactionTypeSpendPrepmt    = "SPEND-PREPAYMENT"
	BankTransactionTypeTransfer       = "TRANSFER"
)

type BankTransaction struct {
	BankTransactionID uuid.UUID        `json:"BankTransactionID"`
	Type              string           `json:"Type"`
	Contact           *Contact         `json:"Contact,omitempty"`
	ContactID         *uuid.UUID       `json:"-"`
	BankAccount       *Account         `json:"BankAccount,omitempty"`
	BankAccountID     *uuid.UUID       `json:"-"`
	IsReconciled      bool             `json:"IsReconciled"`
	Date              *time.Time       `json:"Date,omitempty"`
	Reference         string           `json:"Reference,omitempty"`
	CurrencyCode      string           `json:"CurrencyCode,omitempty"`
	CurrencyRate      *decimal.Decimal `json:"CurrencyRate,omitempty"`
	URL               string           `json:"Url,omitempty"`
	Status            string           `json:"Status"`
	LineAmountTypes   string           `json:"LineAmountTypes"`
	SubTotal          decimal.Decimal  `json:"SubTotal"`
	TotalTax          decimal.Decimal  `json:"TotalTax"`
	Total             decimal.Decimal  `json:"Total"`
	LineItems         []LineItem       `json:"LineItems,omitempty"`
	UpdatedDateUTC    time.Time        `json:"UpdatedDateUTC"`
}

type BankTransfer struct {
	BankTransferID    uuid.UUID        `json:"BankTransferID"`
	FromBankAccountID uuid.UUID        `json:"FromBankAccountID"`
	ToBankAccountID   uuid.UUID        `json:"ToBankAccountID"`
	Amount            decimal.Decimal  `json:"Amount"`
	Date              time.Time        `json:"Date"`
	Reference         string           `json:"Reference,omitempty"`
	CurrencyRate      *decimal.Decimal `json:"CurrencyRate,omitempty"`
	HasAttachments    bool             `json:"HasAttachments"`
	CreatedDateUTC    time.Time        `json:"CreatedDateUTC"`
}
