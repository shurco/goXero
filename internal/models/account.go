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

// Account system roles, exactly as Xero documents them on the Account
// resource (SystemAccount). A role, not a code, is what identifies a control
// account: Xero gives the account the role and leaves the code to the
// organisation, so code "820" in one chart is "Sales Tax" in another and a
// different account entirely in a third.
//
// The list is Xero's own enum, verbatim:
// https://github.com/XeroAPI/Xero-OpenAPI — xero_accounting.yaml, Account.SystemAccount.
const (
	SystemAccountDebtors                = "DEBTORS"
	SystemAccountCreditors              = "CREDITORS"
	SystemAccountBankCurrencyGain       = "BANKCURRENCYGAIN"
	SystemAccountGST                    = "GST"
	SystemAccountGSTOnImports           = "GSTONIMPORTS"
	SystemAccountHistorical             = "HISTORICAL"
	SystemAccountRealisedCurrencyGain   = "REALISEDCURRENCYGAIN"
	SystemAccountRetainedEarnings       = "RETAINEDEARNINGS"
	SystemAccountRounding               = "ROUNDING"
	SystemAccountTrackingTransfers      = "TRACKINGTRANSFERS"
	SystemAccountUnpaidExpClm           = "UNPAIDEXPCLM"
	SystemAccountUnrealisedCurrencyGain = "UNREALISEDCURRENCYGAIN"
	SystemAccountWagePayables           = "WAGEPAYABLES"
)

// Account classes, Xero's Account.Class — the reporting group an account's type
// belongs to. They are the five groups Xero's reports are organised by, and the
// value is a function of the type rather than an independent fact, which is why
// nothing stores it.
const (
	AccountClassAsset     = "ASSET"
	AccountClassEquity    = "EQUITY"
	AccountClassExpense   = "EXPENSE"
	AccountClassLiability = "LIABILITY"
	AccountClassRevenue   = "REVENUE"
)

// AccountClassForType returns the reporting class Xero gives an account of this
// type, or "" for a type outside the enum. Every type has one: a Current Asset is
// an asset, a PAYG Liability is a liability, and Revenue and Sales are both
// revenue — Xero's Class is derived from Type, never set independently.
func AccountClassForType(t string) string {
	switch t {
	case AccountTypeBank, AccountTypeCurrent, AccountTypeFixed, AccountTypeInventory,
		AccountTypeNonCurrent, AccountTypePrepayment:
		return AccountClassAsset
	case AccountTypeCurrLiab, AccountTypeLiability, AccountTypeTermLiab,
		AccountTypePAYGLiab, AccountTypeSuperLiab:
		return AccountClassLiability
	case AccountTypeEquity:
		return AccountClassEquity
	case AccountTypeRevenue, AccountTypeSales:
		return AccountClassRevenue
	case AccountTypeExpense, AccountTypeOverheads, AccountTypeDepreciatn,
		AccountTypeDirectCosts, AccountTypeWages:
		return AccountClassExpense
	}
	return ""
}

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
