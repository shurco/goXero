package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Prepayment — a credit balance taken before an invoice is raised
// (e.g. customer pays a retainer). Mirrors Xero `Prepayments` endpoint.
type Prepayment struct {
	PrepaymentID    uuid.UUID       `json:"PrepaymentID"`
	Type            string          `json:"Type"` // RECEIVE-PREPAYMENT | SPEND-PREPAYMENT
	Status          string          `json:"Status"`
	Contact         *Contact        `json:"Contact,omitempty"`
	ContactID       *uuid.UUID      `json:"ContactID,omitempty"`
	BankAccountID   *uuid.UUID      `json:"BankAccountID,omitempty"`
	CurrencyCode    string          `json:"CurrencyCode"`
	Date            *time.Time      `json:"Date,omitempty"`
	Reference       string          `json:"Reference,omitempty"`
	SubTotal        decimal.Decimal `json:"SubTotal"`
	TotalTax        decimal.Decimal `json:"TotalTax"`
	Total           decimal.Decimal `json:"Total"`
	RemainingCredit decimal.Decimal `json:"RemainingCredit"`
	UpdatedDateUTC  time.Time       `json:"UpdatedDateUTC"`
}

const (
	PrepaymentTypeReceive = "RECEIVE-PREPAYMENT"
	PrepaymentTypeSpend   = "SPEND-PREPAYMENT"
)

// Overpayment — the reverse case: customer paid too much, the excess is held
// as credit. Xero exposes a separate endpoint for this.
type Overpayment struct {
	OverpaymentID   uuid.UUID       `json:"OverpaymentID"`
	Type            string          `json:"Type"` // RECEIVE-OVERPAYMENT | SPEND-OVERPAYMENT
	Status          string          `json:"Status"`
	Contact         *Contact        `json:"Contact,omitempty"`
	ContactID       *uuid.UUID      `json:"ContactID,omitempty"`
	BankAccountID   *uuid.UUID      `json:"BankAccountID,omitempty"`
	CurrencyCode    string          `json:"CurrencyCode"`
	Date            *time.Time      `json:"Date,omitempty"`
	Reference       string          `json:"Reference,omitempty"`
	Total           decimal.Decimal `json:"Total"`
	RemainingCredit decimal.Decimal `json:"RemainingCredit"`
	UpdatedDateUTC  time.Time       `json:"UpdatedDateUTC"`
}

const (
	OverpaymentTypeReceive = "RECEIVE-OVERPAYMENT"
	OverpaymentTypeSpend   = "SPEND-OVERPAYMENT"
)

// RepeatingInvoice is a recurring invoice template. The `Schedule` is flattened
// onto the top-level struct rather than nested to match Xero's JSON shape.
type RepeatingInvoice struct {
	RepeatingInvoiceID uuid.UUID       `json:"RepeatingInvoiceID"`
	Type               string          `json:"Type"` // ACCREC | ACCPAY
	Status             string          `json:"Status"`
	Contact            *Contact        `json:"Contact,omitempty"`
	ContactID          *uuid.UUID      `json:"ContactID,omitempty"`
	Reference          string          `json:"Reference,omitempty"`
	LineAmountTypes    string          `json:"LineAmountTypes"`
	CurrencyCode       string          `json:"CurrencyCode"`
	BrandingThemeID    *uuid.UUID      `json:"BrandingThemeID,omitempty"`
	Schedule           Schedule        `json:"Schedule"`
	SubTotal           decimal.Decimal `json:"SubTotal"`
	TotalTax           decimal.Decimal `json:"TotalTax"`
	Total              decimal.Decimal `json:"Total"`
	LineItems          []LineItem      `json:"LineItems,omitempty"`
	UpdatedDateUTC     time.Time       `json:"UpdatedDateUTC"`
}

// Schedule — how often and when a repeating invoice is generated.
type Schedule struct {
	Period            int        `json:"Period"`
	Unit              string     `json:"Unit"` // WEEKLY | MONTHLY | YEARLY
	DueDate           int        `json:"DueDate"`
	DueDateType       string     `json:"DueDateType"`
	StartDate         *time.Time `json:"StartDate,omitempty"`
	NextScheduledDate *time.Time `json:"NextScheduledDate,omitempty"`
	EndDate           *time.Time `json:"EndDate,omitempty"`
}

// BatchPayment groups many payments into one bank deposit / withdrawal
// line. Children live in `payments.batch_payment_id`.
type BatchPayment struct {
	BatchPaymentID uuid.UUID       `json:"BatchPaymentID"`
	AccountID      uuid.UUID       `json:"AccountID"`
	Date           *time.Time      `json:"Date,omitempty"`
	Reference      string          `json:"Reference,omitempty"`
	Narrative      string          `json:"Narrative,omitempty"`
	Details        string          `json:"Details,omitempty"`
	Status         string          `json:"Status"`
	TotalAmount    decimal.Decimal `json:"TotalAmount"`
	Payments       []Payment       `json:"Payments,omitempty"`
	UpdatedDateUTC time.Time       `json:"UpdatedDateUTC"`
}

// LinkedTransaction connects a bill line (source) to an invoice line
// (target) — "re-bill this supplier cost to the customer".
type LinkedTransaction struct {
	LinkedTransactionID uuid.UUID  `json:"LinkedTransactionID"`
	SourceTransactionID uuid.UUID  `json:"SourceTransactionID"`
	SourceLineItemID    *uuid.UUID `json:"SourceLineItemID,omitempty"`
	TargetTransactionID *uuid.UUID `json:"TargetTransactionID,omitempty"`
	TargetLineItemID    *uuid.UUID `json:"TargetLineItemID,omitempty"`
	ContactID           *uuid.UUID `json:"ContactID,omitempty"`
	Type                string     `json:"Type"`
	Status              string     `json:"Status"`
	UpdatedDateUTC      time.Time  `json:"UpdatedDateUTC"`
}

// Employee — simple HR record used by Receipts and Expense claims.
type Employee struct {
	EmployeeID     uuid.UUID `json:"EmployeeID"`
	FirstName      string    `json:"FirstName"`
	LastName       string    `json:"LastName,omitempty"`
	Email          string    `json:"Email,omitempty"`
	Phone          string    `json:"Phone,omitempty"`
	Status         string    `json:"Status"`
	UpdatedDateUTC time.Time `json:"UpdatedDateUTC"`
}

// Receipt is an expense-claim receipt raised by a user.
type Receipt struct {
	ReceiptID       uuid.UUID       `json:"ReceiptID"`
	UserID          *uuid.UUID      `json:"UserID,omitempty"`
	Contact         *Contact        `json:"Contact,omitempty"`
	ContactID       *uuid.UUID      `json:"ContactID,omitempty"`
	Date            *time.Time      `json:"Date,omitempty"`
	Reference       string          `json:"Reference,omitempty"`
	Status          string          `json:"Status"`
	LineAmountTypes string          `json:"LineAmountTypes"`
	SubTotal        decimal.Decimal `json:"SubTotal"`
	TotalTax        decimal.Decimal `json:"TotalTax"`
	Total           decimal.Decimal `json:"Total"`
	LineItems       []LineItem      `json:"LineItems,omitempty"`
	UpdatedDateUTC  time.Time       `json:"UpdatedDateUTC"`
}

// ExpenseClaim — a bundle of receipts submitted by a user.
type ExpenseClaim struct {
	ExpenseClaimID uuid.UUID       `json:"ExpenseClaimID"`
	UserID         *uuid.UUID      `json:"UserID,omitempty"`
	Status         string          `json:"Status"`
	PaymentDueDate *time.Time      `json:"PaymentDueDate,omitempty"`
	ReportingDate  *time.Time      `json:"ReportingDate,omitempty"`
	Total          decimal.Decimal `json:"Total"`
	AmountDue      decimal.Decimal `json:"AmountDue"`
	AmountPaid     decimal.Decimal `json:"AmountPaid"`
	Receipts       []Receipt       `json:"Receipts,omitempty"`
	UpdatedDateUTC time.Time       `json:"UpdatedDateUTC"`
}
