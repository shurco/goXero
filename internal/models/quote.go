package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	QuoteStatusDraft    = "DRAFT"
	QuoteStatusSent     = "SENT"
	QuoteStatusDeclined = "DECLINED"
	QuoteStatusAccepted = "ACCEPTED"
	QuoteStatusInvoiced = "INVOICED"
	QuoteStatusDeleted  = "DELETED"
)

type Quote struct {
	QuoteID         uuid.UUID        `json:"QuoteID"`
	Contact         *Contact         `json:"Contact,omitempty"`
	ContactID       *uuid.UUID       `json:"-"`
	QuoteNumber     string           `json:"QuoteNumber,omitempty"`
	Reference       string           `json:"Reference,omitempty"`
	Title           string           `json:"Title,omitempty"`
	Summary         string           `json:"Summary,omitempty"`
	Terms           string           `json:"Terms,omitempty"`
	Date            *time.Time       `json:"Date,omitempty"`
	ExpiryDate      *time.Time       `json:"ExpiryDate,omitempty"`
	Status          string           `json:"Status"`
	LineAmountTypes string           `json:"LineAmountTypes"`
	CurrencyCode    string           `json:"CurrencyCode,omitempty"`
	CurrencyRate    *decimal.Decimal `json:"CurrencyRate,omitempty"`
	BrandingThemeID *uuid.UUID       `json:"BrandingThemeID,omitempty"`
	SubTotal        decimal.Decimal  `json:"SubTotal"`
	TotalTax        decimal.Decimal  `json:"TotalTax"`
	Total           decimal.Decimal  `json:"Total"`
	TotalDiscount   decimal.Decimal  `json:"TotalDiscount"`
	LineItems       []LineItem       `json:"LineItems,omitempty"`
	UpdatedDateUTC  time.Time        `json:"UpdatedDateUTC"`
}

const (
	PurchaseOrderStatusDraft      = "DRAFT"
	PurchaseOrderStatusSubmitted  = "SUBMITTED"
	PurchaseOrderStatusAuthorised = "AUTHORISED"
	PurchaseOrderStatusBilled     = "BILLED"
	PurchaseOrderStatusDeleted    = "DELETED"
)

type PurchaseOrder struct {
	PurchaseOrderID      uuid.UUID        `json:"PurchaseOrderID"`
	Contact              *Contact         `json:"Contact,omitempty"`
	ContactID            *uuid.UUID       `json:"-"`
	PurchaseOrderNumber  string           `json:"PurchaseOrderNumber,omitempty"`
	Reference            string           `json:"Reference,omitempty"`
	Date                 *time.Time       `json:"Date,omitempty"`
	DeliveryDate         *time.Time       `json:"DeliveryDate,omitempty"`
	DeliveryAddress      string           `json:"DeliveryAddress,omitempty"`
	AttentionTo          string           `json:"AttentionTo,omitempty"`
	Telephone            string           `json:"Telephone,omitempty"`
	DeliveryInstructions string           `json:"DeliveryInstructions,omitempty"`
	Status               string           `json:"Status"`
	LineAmountTypes      string           `json:"LineAmountTypes"`
	CurrencyCode         string           `json:"CurrencyCode,omitempty"`
	CurrencyRate         *decimal.Decimal `json:"CurrencyRate,omitempty"`
	SubTotal             decimal.Decimal  `json:"SubTotal"`
	TotalTax             decimal.Decimal  `json:"TotalTax"`
	Total                decimal.Decimal  `json:"Total"`
	LineItems            []LineItem       `json:"LineItems,omitempty"`
	UpdatedDateUTC       time.Time        `json:"UpdatedDateUTC"`
}
