package repository

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/shurco/goxero/internal/models"
)

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func decPtr(s string) *decimal.Decimal {
	d := dec(s)
	return &d
}

func TestRecalculateTotalsExclusive(t *testing.T) {
	inv := &models.Invoice{
		LineAmountTypes: models.LineAmountTypesExclusive,
		LineItems: []models.LineItem{
			{
				Quantity:   dec("2"),
				UnitAmount: dec("100"),
				TaxAmount:  dec("40"),
			},
			{
				Quantity:     dec("1"),
				UnitAmount:   dec("50"),
				TaxAmount:    dec("5"),
				DiscountRate: decPtr("10"),
			},
		},
	}

	recalculateTotals(inv)

	assert.True(t, inv.SubTotal.Equal(dec("245")))
	assert.True(t, inv.TotalTax.Equal(dec("45")))
	assert.True(t, inv.TotalDiscount.Equal(dec("5")))
	assert.True(t, inv.Total.Equal(dec("290")))
	assert.True(t, inv.LineItems[1].LineAmount.Equal(dec("45")))
}

func TestRecalculateTotalsInclusive(t *testing.T) {
	inv := &models.Invoice{
		LineAmountTypes: models.LineAmountTypesInclusive,
		LineItems: []models.LineItem{
			{Quantity: dec("3"), UnitAmount: dec("20"), TaxAmount: dec("6")},
		},
	}
	recalculateTotals(inv)

	assert.True(t, inv.Total.Equal(dec("60")))
	assert.True(t, inv.TotalTax.Equal(dec("6")))
}

func TestRecalculateTotalsNoTax(t *testing.T) {
	inv := &models.Invoice{
		LineAmountTypes: models.LineAmountTypesNoTax,
		LineItems: []models.LineItem{
			{Quantity: dec("1"), UnitAmount: dec("10"), TaxAmount: dec("123")},
		},
	}
	recalculateTotals(inv)

	assert.True(t, inv.TotalTax.IsZero())
	assert.True(t, inv.Total.Equal(dec("10")))
}

func TestRecalculateTotalsDefaultsQuantity(t *testing.T) {
	inv := &models.Invoice{
		LineAmountTypes: models.LineAmountTypesExclusive,
		LineItems: []models.LineItem{
			{Quantity: decimal.Zero, UnitAmount: dec("42")},
		},
	}
	recalculateTotals(inv)

	assert.True(t, inv.LineItems[0].Quantity.Equal(decimal.NewFromInt(1)))
	assert.True(t, inv.Total.Equal(dec("42")))
}

func TestRecalculateTotalsDiscountAmount(t *testing.T) {
	inv := &models.Invoice{
		LineAmountTypes: models.LineAmountTypesExclusive,
		LineItems: []models.LineItem{
			{
				Quantity:       dec("1"),
				UnitAmount:     dec("200"),
				DiscountAmount: decPtr("25"),
			},
		},
	}
	recalculateTotals(inv)

	assert.True(t, inv.SubTotal.Equal(dec("175")))
	assert.True(t, inv.TotalDiscount.Equal(dec("25")))
}
