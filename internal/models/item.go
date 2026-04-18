package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Item struct {
	ItemID                    uuid.UUID         `json:"ItemID"`
	Code                      string            `json:"Code"`
	Name                      string            `json:"Name,omitempty"`
	Description               string            `json:"Description,omitempty"`
	PurchaseDescription       string            `json:"PurchaseDescription,omitempty"`
	IsTrackedAsInventory      bool              `json:"IsTrackedAsInventory"`
	IsSold                    bool              `json:"IsSold"`
	IsPurchased               bool              `json:"IsPurchased"`
	InventoryAssetAccountCode string            `json:"InventoryAssetAccountCode,omitempty"`
	QuantityOnHand            decimal.Decimal   `json:"QuantityOnHand"`
	SalesDetails              *ItemPriceDetails `json:"SalesDetails,omitempty"`
	PurchaseDetails           *ItemPriceDetails `json:"PurchaseDetails,omitempty"`
	TotalCostPool             *decimal.Decimal  `json:"TotalCostPool,omitempty"`
	UpdatedDateUTC            time.Time         `json:"UpdatedDateUTC"`
}

type ItemPriceDetails struct {
	UnitPrice   *decimal.Decimal `json:"UnitPrice,omitempty"`
	AccountCode string           `json:"AccountCode,omitempty"`
	TaxType     string           `json:"TaxType,omitempty"`
}
