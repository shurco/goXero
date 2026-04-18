package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Account types as defined by Xero.
const (
	AccountTypeBank        = "BANK"
	AccountTypeCurrent     = "CURRENT"
	AccountTypeCurrLiab    = "CURRLIAB"
	AccountTypeDepreciatn  = "DEPRECIATN"
	AccountTypeDirectCosts = "DIRECTCOSTS"
	AccountTypeEquity      = "EQUITY"
	AccountTypeExpense     = "EXPENSE"
	AccountTypeFixed       = "FIXED"
	AccountTypeInventory   = "INVENTORY"
	AccountTypeLiability   = "LIABILITY"
	AccountTypeNonCurrent  = "NONCURRENT"
	AccountTypeOverheads   = "OVERHEADS"
	AccountTypePrepayment  = "PREPAYMENT"
	AccountTypeRevenue     = "REVENUE"
	AccountTypeSales       = "SALES"
	AccountTypeTermLiab    = "TERMLIAB"
	AccountTypePAYGLiab    = "PAYGLIABILITY"
	AccountTypeSuperLiab   = "SUPERANNUATIONLIABILITY"
	AccountTypeWages       = "WAGESEXPENSE"
)

type Account struct {
	AccountID               uuid.UUID `json:"AccountID"`
	Code                    string    `json:"Code"`
	Name                    string    `json:"Name"`
	Type                    string    `json:"Type"`
	BankAccountNumber       string    `json:"BankAccountNumber,omitempty"`
	BankAccountType         string    `json:"BankAccountType,omitempty"`
	CurrencyCode            string    `json:"CurrencyCode,omitempty"`
	Status                  string    `json:"Status"`
	Description             string    `json:"Description,omitempty"`
	TaxType                 string    `json:"TaxType,omitempty"`
	EnablePaymentsToAccount bool      `json:"EnablePaymentsToAccount"`
	ShowInExpenseClaims     bool      `json:"ShowInExpenseClaims"`
	Class                   string    `json:"Class,omitempty"`
	SystemAccount           string    `json:"SystemAccount,omitempty"`
	ReportingCode           string    `json:"ReportingCode,omitempty"`
	ReportingCodeName       string    `json:"ReportingCodeName,omitempty"`
	HasAttachments          bool      `json:"HasAttachments"`
	UpdatedDateUTC          time.Time `json:"UpdatedDateUTC"`
}

type TaxRate struct {
	TaxRateID             uuid.UUID       `json:"TaxRateID"`
	Name                  string          `json:"Name"`
	TaxType               string          `json:"TaxType"`
	ReportTaxType         string          `json:"ReportTaxType,omitempty"`
	CanApplyToAssets      bool            `json:"CanApplyToAssets"`
	CanApplyToEquity      bool            `json:"CanApplyToEquity"`
	CanApplyToExpenses    bool            `json:"CanApplyToExpenses"`
	CanApplyToLiabilities bool            `json:"CanApplyToLiabilities"`
	CanApplyToRevenue     bool            `json:"CanApplyToRevenue"`
	DisplayTaxRate        decimal.Decimal `json:"DisplayTaxRate"`
	EffectiveRate         decimal.Decimal `json:"EffectiveRate"`
	Status                string          `json:"Status"`
}

type Currency struct {
	Code        string `json:"Code"`
	Description string `json:"Description,omitempty"`
}
