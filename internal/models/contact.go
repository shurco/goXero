package models

import (
	"time"

	"github.com/google/uuid"
)

type Contact struct {
	ContactID                 uuid.UUID `json:"ContactID"`
	ContactNumber             string    `json:"ContactNumber,omitempty"`
	AccountNumber             string    `json:"AccountNumber,omitempty"`
	ContactStatus             string    `json:"ContactStatus"`
	Name                      string    `json:"Name"`
	FirstName                 string    `json:"FirstName,omitempty"`
	LastName                  string    `json:"LastName,omitempty"`
	CompanyNumber             string    `json:"CompanyNumber,omitempty"`
	EmailAddress              string    `json:"EmailAddress,omitempty"`
	SkypeUserName             string    `json:"SkypeUserName,omitempty"`
	BankAccountDetails        string    `json:"BankAccountDetails,omitempty"`
	TaxNumber                 string    `json:"TaxNumber,omitempty"`
	AccountsReceivableTaxType string    `json:"AccountsReceivableTaxType,omitempty"`
	AccountsPayableTaxType    string    `json:"AccountsPayableTaxType,omitempty"`
	IsSupplier                bool      `json:"IsSupplier"`
	IsCustomer                bool      `json:"IsCustomer"`
	DefaultCurrency           string    `json:"DefaultCurrency,omitempty"`
	Website                   string    `json:"Website,omitempty"`
	HasAttachments            bool      `json:"HasAttachments"`
	Addresses                 []Address `json:"Addresses,omitempty"`
	Phones                    []Phone   `json:"Phones,omitempty"`
	UpdatedDateUTC            time.Time `json:"UpdatedDateUTC"`
}
