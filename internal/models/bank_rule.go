package models

import (
	"time"

	"github.com/google/uuid"
)

// BankRule automates categorisation during bank reconciliation (Xero-style).
type BankRule struct {
	BankRuleID   uuid.UUID          `json:"BankRuleID"`
	RuleType     string             `json:"RuleType"`
	Name         string             `json:"Name"`
	Definition   BankRuleDefinition `json:"Definition"`
	IsActive     bool               `json:"IsActive"`
	CreatedAt    time.Time          `json:"CreatedDateUTC"`
	UpdatedAt    time.Time          `json:"UpdatedDateUTC"`
}

// BankRuleCondition matches a bank statement field.
type BankRuleCondition struct {
	Field    string `json:"Field"`
	Operator string `json:"Operator"`
	Value    string `json:"Value"`
}

// BankRuleAllocationLine is a split line (fixed amount or percent of remainder).
type BankRuleAllocationLine struct {
	LineID      string  `json:"LineID,omitempty"`
	Description string  `json:"Description,omitempty"`
	AccountID   string  `json:"AccountID,omitempty"`
	TaxRateID   string  `json:"TaxRateID,omitempty"`
	Region      string  `json:"Region,omitempty"`
	Amount      float64 `json:"Amount,omitempty"`
	Percent     float64 `json:"Percent,omitempty"`
}

// BankRuleDefinition holds the rule body stored as JSONB.
type BankRuleDefinition struct {
	MatchMode   string `json:"MatchMode"`
	Conditions  []BankRuleCondition `json:"Conditions"`
	ContactMode string `json:"ContactMode,omitempty"`
	ContactID   string `json:"ContactID,omitempty"`
	ContactName string `json:"ContactName,omitempty"`
	FixedLines  []BankRuleAllocationLine `json:"FixedLines,omitempty"`
	PercentLines []BankRuleAllocationLine `json:"PercentLines,omitempty"`
	ReferenceField string `json:"ReferenceField,omitempty"`
	RunOn          string `json:"RunOn"`
	ScopeBankAccountID string `json:"ScopeBankAccountID,omitempty"`
	TransferTargetMode     string `json:"TransferTargetMode,omitempty"`
	TransferBankAccountID  string `json:"TransferBankAccountID,omitempty"`
	TransferTrackingRegion string `json:"TransferTrackingRegion,omitempty"`
}
