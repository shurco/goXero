package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type ContactGroup struct {
	ContactGroupID uuid.UUID `json:"ContactGroupID"`
	Name           string    `json:"Name"`
	Status         string    `json:"Status"`
	Contacts       []Contact `json:"Contacts,omitempty"`
}

type BrandingTheme struct {
	BrandingThemeID uuid.UUID `json:"BrandingThemeID"`
	Name            string    `json:"Name"`
	SortOrder       int       `json:"SortOrder"`
	LogoURL         string    `json:"LogoUrl,omitempty"`
	CreatedDateUTC  time.Time `json:"CreatedDateUTC"`
}

type TrackingCategory struct {
	TrackingCategoryID uuid.UUID        `json:"TrackingCategoryID"`
	Name               string           `json:"Name"`
	Status             string           `json:"Status"`
	Options            []TrackingOption `json:"Options,omitempty"`
}

type TrackingOption struct {
	TrackingOptionID uuid.UUID `json:"TrackingOptionID"`
	Name             string    `json:"Name"`
	Status           string    `json:"Status"`
}

type Attachment struct {
	AttachmentID  uuid.UUID `json:"AttachmentID"`
	FileName      string    `json:"FileName"`
	MimeType      string    `json:"MimeType,omitempty"`
	ContentLength int64     `json:"ContentLength"`
	IncludeOnline bool      `json:"IncludeOnline"`
	URL           string    `json:"Url,omitempty"`
}

type HistoryRecord struct {
	HistoryID uuid.UUID `json:"-"`
	Changes   string    `json:"Changes"`
	Details   string    `json:"Details"`
	DateUTC   time.Time `json:"DateUTCString"`
	User      string    `json:"User,omitempty"`
}

// CreditNoteAllocation links a credit note to an invoice it pays down.
type CreditNoteAllocation struct {
	AllocationID uuid.UUID       `json:"AllocationID"`
	InvoiceID    uuid.UUID       `json:"InvoiceID"`
	Amount       decimal.Decimal `json:"Amount"`
	Date         time.Time       `json:"Date"`
}
