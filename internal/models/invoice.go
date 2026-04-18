package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	InvoiceTypeAccRec = "ACCREC"
	InvoiceTypeAccPay = "ACCPAY"

	InvoiceStatusDraft      = "DRAFT"
	InvoiceStatusSubmitted  = "SUBMITTED"
	InvoiceStatusAuthorised = "AUTHORISED"
	InvoiceStatusPaid       = "PAID"
	InvoiceStatusVoided     = "VOIDED"
	InvoiceStatusDeleted    = "DELETED"

	LineAmountTypesExclusive = "Exclusive"
	LineAmountTypesInclusive = "Inclusive"
	LineAmountTypesNoTax     = "NoTax"
)

type Invoice struct {
	InvoiceID           uuid.UUID        `json:"InvoiceID"`
	Type                string           `json:"Type"`
	ContactID           *uuid.UUID       `json:"-"`
	Contact             *Contact         `json:"Contact,omitempty"`
	InvoiceNumber       string           `json:"InvoiceNumber,omitempty"`
	Reference           string           `json:"Reference,omitempty"`
	BrandingThemeID     *uuid.UUID       `json:"BrandingThemeID,omitempty"`
	URL                 string           `json:"Url,omitempty"`
	CurrencyCode        string           `json:"CurrencyCode,omitempty"`
	CurrencyRate        *decimal.Decimal `json:"CurrencyRate,omitempty"`
	Status              string           `json:"Status"`
	LineAmountTypes     string           `json:"LineAmountTypes"`
	Date                *time.Time       `json:"Date,omitempty"`
	DueDate             *time.Time       `json:"DueDate,omitempty"`
	ExpectedPaymentDate *time.Time       `json:"ExpectedPaymentDate,omitempty"`
	PlannedPaymentDate  *time.Time       `json:"PlannedPaymentDate,omitempty"`
	FullyPaidOnDate     *time.Time       `json:"FullyPaidOnDate,omitempty"`
	SubTotal            decimal.Decimal  `json:"SubTotal"`
	TotalTax            decimal.Decimal  `json:"TotalTax"`
	Total               decimal.Decimal  `json:"Total"`
	TotalDiscount       decimal.Decimal  `json:"TotalDiscount"`
	AmountDue           decimal.Decimal  `json:"AmountDue"`
	AmountPaid          decimal.Decimal  `json:"AmountPaid"`
	AmountCredited      decimal.Decimal  `json:"AmountCredited"`
	HasAttachments      bool             `json:"HasAttachments"`
	SentToContact       bool             `json:"SentToContact"`
	IsDiscounted        bool             `json:"IsDiscounted"`
	LineItems           []LineItem       `json:"LineItems,omitempty"`
	Payments            []Payment        `json:"Payments,omitempty"`
	UpdatedDateUTC      time.Time        `json:"UpdatedDateUTC"`
}

type LineItem struct {
	LineItemID     uuid.UUID        `json:"LineItemID"`
	Description    string           `json:"Description,omitempty"`
	Quantity       decimal.Decimal  `json:"Quantity"`
	UnitAmount     decimal.Decimal  `json:"UnitAmount"`
	ItemCode       string           `json:"ItemCode,omitempty"`
	AccountCode    string           `json:"AccountCode,omitempty"`
	TaxType        string           `json:"TaxType,omitempty"`
	TaxAmount      decimal.Decimal  `json:"TaxAmount"`
	LineAmount     decimal.Decimal  `json:"LineAmount"`
	DiscountRate   *decimal.Decimal `json:"DiscountRate,omitempty"`
	DiscountAmount *decimal.Decimal `json:"DiscountAmount,omitempty"`
	SortOrder      int              `json:"-"`
}

const (
	CreditNoteTypeAccRecCredit = "ACCRECCREDIT"
	CreditNoteTypeAccPayCredit = "ACCPAYCREDIT"

	CreditNoteStatusDraft      = "DRAFT"
	CreditNoteStatusSubmitted  = "SUBMITTED"
	CreditNoteStatusAuthorised = "AUTHORISED"
	CreditNoteStatusPaid       = "PAID"
	CreditNoteStatusVoided     = "VOIDED"
	CreditNoteStatusDeleted    = "DELETED"
)

type CreditNote struct {
	CreditNoteID     uuid.UUID              `json:"CreditNoteID"`
	Type             string                 `json:"Type"`
	ContactID        *uuid.UUID             `json:"-"`
	Contact          *Contact               `json:"Contact,omitempty"`
	CreditNoteNumber string                 `json:"CreditNoteNumber,omitempty"`
	Reference        string                 `json:"Reference,omitempty"`
	Status           string                 `json:"Status"`
	Date             *time.Time             `json:"Date,omitempty"`
	DueDate          *time.Time             `json:"DueDate,omitempty"`
	CurrencyCode     string                 `json:"CurrencyCode,omitempty"`
	CurrencyRate     *decimal.Decimal       `json:"CurrencyRate,omitempty"`
	LineAmountTypes  string                 `json:"LineAmountTypes"`
	SubTotal         decimal.Decimal        `json:"SubTotal"`
	TotalTax         decimal.Decimal        `json:"TotalTax"`
	Total            decimal.Decimal        `json:"Total"`
	RemainingCredit  decimal.Decimal        `json:"RemainingCredit"`
	LineItems        []LineItem             `json:"LineItems,omitempty"`
	Allocations      []CreditNoteAllocation `json:"Allocations,omitempty"`
	UpdatedDateUTC   time.Time              `json:"UpdatedDateUTC"`
}

type Payment struct {
	PaymentID      uuid.UUID        `json:"PaymentID"`
	InvoiceID      *uuid.UUID       `json:"-"`
	CreditNoteID   *uuid.UUID       `json:"-"`
	AccountID      *uuid.UUID       `json:"-"`
	Invoice        *Invoice         `json:"Invoice,omitempty"`
	Account        *Account         `json:"Account,omitempty"`
	PaymentType    string           `json:"PaymentType"`
	Status         string           `json:"Status"`
	Date           time.Time        `json:"Date"`
	CurrencyRate   *decimal.Decimal `json:"CurrencyRate,omitempty"`
	Amount         decimal.Decimal  `json:"Amount"`
	Reference      string           `json:"Reference,omitempty"`
	IsReconciled   bool             `json:"IsReconciled"`
	UpdatedDateUTC time.Time        `json:"UpdatedDateUTC"`
}
