package models

import (
	"time"

	"github.com/google/uuid"
)

type Organisation struct {
	OrganisationID        uuid.UUID `json:"OrganisationID"`
	APIKey                string    `json:"APIKey,omitempty"`
	Name                  string    `json:"Name"`
	LegalName             string    `json:"LegalName,omitempty"`
	ShortCode             string    `json:"ShortCode,omitempty"`
	OrganisationType      string    `json:"OrganisationType,omitempty"`
	CountryCode           string    `json:"CountryCode,omitempty"`
	BaseCurrency          string    `json:"BaseCurrency"`
	Timezone              string    `json:"Timezone,omitempty"`
	FinancialYearEndDay   int       `json:"FinancialYearEndDay,omitempty"`
	FinancialYearEndMonth int       `json:"FinancialYearEndMonth,omitempty"`
	TaxNumber             string    `json:"TaxNumber,omitempty"`
	LineOfBusiness        string    `json:"LineOfBusiness,omitempty"`
	RegistrationNumber    string    `json:"RegistrationNumber,omitempty"`
	IsDemoCompany         bool      `json:"IsDemoCompany"`
	OrganisationStatus    string    `json:"OrganisationStatus"`
	CreatedAt             time.Time `json:"CreatedDateUTC"`
	UpdatedAt             time.Time `json:"UpdatedDateUTC"`
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
