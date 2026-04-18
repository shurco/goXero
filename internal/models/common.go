package models

import (
	"time"

	"github.com/shopspring/decimal"
)

// Pagination is the default envelope used by list endpoints.
type Pagination struct {
	Page     int `json:"page"     query:"page"`
	PageSize int `json:"pageSize" query:"pageSize"`
	Total    int `json:"total"`
}

func (p *Pagination) Normalize() {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 {
		p.PageSize = 50
	}
	if p.PageSize > 200 {
		p.PageSize = 200
	}
}

func (p Pagination) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// APIResponse is the canonical Xero-like envelope.
type APIResponse struct {
	ID           string      `json:"Id"`
	Status       string      `json:"Status"`
	ProviderName string      `json:"ProviderName,omitempty"`
	DateTimeUTC  time.Time   `json:"DateTimeUTC"`
	Payload      interface{} `json:"Payload,omitempty"`
}

// ErrorResponse follows the Xero API validation error shape.
type ErrorResponse struct {
	ErrorNumber int              `json:"ErrorNumber"`
	Type        string           `json:"Type"`
	Message     string           `json:"Message"`
	Elements    []ValidationItem `json:"Elements,omitempty"`
}

type ValidationItem struct {
	Field   string `json:"Field"`
	Message string `json:"Message"`
}

type Address struct {
	ID           int64  `json:"-"`
	AddressType  string `json:"AddressType"`
	AddressLine1 string `json:"AddressLine1,omitempty"`
	AddressLine2 string `json:"AddressLine2,omitempty"`
	AddressLine3 string `json:"AddressLine3,omitempty"`
	AddressLine4 string `json:"AddressLine4,omitempty"`
	City         string `json:"City,omitempty"`
	Region       string `json:"Region,omitempty"`
	PostalCode   string `json:"PostalCode,omitempty"`
	Country      string `json:"Country,omitempty"`
	AttentionTo  string `json:"AttentionTo,omitempty"`
}

type Phone struct {
	ID               int64  `json:"-"`
	PhoneType        string `json:"PhoneType"`
	PhoneNumber      string `json:"PhoneNumber,omitempty"`
	PhoneAreaCode    string `json:"PhoneAreaCode,omitempty"`
	PhoneCountryCode string `json:"PhoneCountryCode,omitempty"`
}

type Money = decimal.Decimal
