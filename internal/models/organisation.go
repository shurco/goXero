package models

import (
	"time"

	"github.com/google/uuid"
)

type Organisation struct {
	OrganisationID        uuid.UUID           `json:"OrganisationID"`
	APIKey                string              `json:"APIKey,omitempty"`
	Name                  string              `json:"Name"`
	LegalName             string              `json:"LegalName,omitempty"`
	ShortCode             string              `json:"ShortCode,omitempty"`
	OrganisationType      string              `json:"OrganisationType,omitempty"`
	CountryCode           string              `json:"CountryCode,omitempty"`
	BaseCurrency          string              `json:"BaseCurrency"`
	Timezone              string              `json:"Timezone,omitempty"`
	FinancialYearEndDay   int                 `json:"FinancialYearEndDay,omitempty"`
	FinancialYearEndMonth int                 `json:"FinancialYearEndMonth,omitempty"`
	TaxNumber             string              `json:"TaxNumber,omitempty"`
	LineOfBusiness        string              `json:"LineOfBusiness,omitempty"`
	RegistrationNumber    string              `json:"RegistrationNumber,omitempty"`
	Description           string              `json:"Description,omitempty"`
	Profile               OrganisationProfile `json:"Profile,omitempty"`
	IsDemoCompany         bool                `json:"IsDemoCompany"`
	OrganisationStatus    string              `json:"OrganisationStatus"`
	CreatedAt             time.Time           `json:"CreatedDateUTC"`
	UpdatedAt             time.Time           `json:"UpdatedDateUTC"`
}

// OrganisationProfile holds contact & display settings not mapped to top-level columns.
type OrganisationProfile struct {
	ShowExtraOnInvoices bool                `json:"ShowExtraOnInvoices,omitempty"`
	SameAsPostal        bool                `json:"SameAsPostal,omitempty"`
	Postal              OrganisationAddress `json:"Postal,omitempty"`
	Physical            OrganisationAddress `json:"Physical,omitempty"`
	Telephone           OrganisationPhone   `json:"Telephone,omitempty"`
	Mobile              OrganisationPhone   `json:"Mobile,omitempty"`
	Fax                 OrganisationPhone   `json:"Fax,omitempty"`
	Email               string              `json:"Email,omitempty"`
	Website             string              `json:"Website,omitempty"`
	Social              OrganisationSocial  `json:"Social,omitempty"`
	ReplyAddresses      []ReplyEmailAddress `json:"ReplyAddresses,omitempty"`
	EmailTemplates      []OrgEmailTemplate  `json:"EmailTemplates,omitempty"`
	// Onboarding / preferences (stored in profile JSON).
	HasEmployees        *bool  `json:"HasEmployees,omitempty"`
	PriorAccountingTool string `json:"PriorAccountingTool,omitempty"`
}

// ReplyEmailAddress is an extra "reply-to" identity beyond the logged-in user.
type ReplyEmailAddress struct {
	ID    string `json:"ID,omitempty"`
	Email string `json:"Email,omitempty"`
	Name  string `json:"Name,omitempty"`
}

// OrgEmailTemplate stores subject/body patterns for outbound emails (placeholders as plain text).
type OrgEmailTemplate struct {
	ID        string `json:"ID,omitempty"`
	Type      string `json:"Type,omitempty"`
	Name      string `json:"Name,omitempty"`
	IsDefault bool   `json:"IsDefault,omitempty"`
	Subject   string `json:"Subject,omitempty"`
	Body      string `json:"Body,omitempty"`
}

// OrganisationAddress mirrors Xero-style address blocks.
type OrganisationAddress struct {
	AddressLine1 string `json:"AddressLine1,omitempty"`
	City         string `json:"City,omitempty"`
	Region       string `json:"Region,omitempty"`
	PostalCode   string `json:"PostalCode,omitempty"`
	Country      string `json:"Country,omitempty"`
	Attention    string `json:"Attention,omitempty"`
}

// OrganisationPhone is a dial code + local number pair.
type OrganisationPhone struct {
	PhoneCountryCode string `json:"PhoneCountryCode,omitempty"`
	PhoneNumber      string `json:"PhoneNumber,omitempty"`
}

// OrganisationSocial stores social profile suffixes or full handles.
type OrganisationSocial struct {
	Facebook string `json:"Facebook,omitempty"`
	Twitter  string `json:"Twitter,omitempty"`
	LinkedIn string `json:"LinkedIn,omitempty"`
}

type User struct {
	UserID           uuid.UUID `json:"UserID"`
	Email            string    `json:"EmailAddress"`
	FirstName        string    `json:"FirstName,omitempty"`
	LastName         string    `json:"LastName,omitempty"`
	IsSubscriber     bool      `json:"IsSubscriber"`
	OrganisationRole string    `json:"OrganisationRole"`
	CreatedAt        time.Time `json:"CreatedDateUTC"`
}
