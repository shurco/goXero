package repository

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/shurco/goxero/internal/models"
)

// A document journal balances only when the control line — the document total —
// equals the sum of every other line. Inclusive amounts are the trap: the total
// already contains the tax, so a line that carries its full amount *and* a
// separate tax line unbalances the journal by exactly the tax. That is what made
// authorising a tax-inclusive invoice fail with "unbalanced journal".
func TestDocumentLineNetKeepsTheJournalBalanced(t *testing.T) {
	tax := decimal.RequireFromString("6")
	cases := []struct {
		name        string
		amountTypes string
		lineAmount  string // the figure on the line, tax in it or not
		total       string // the document total the control line carries
		taxAccOK    bool
		wantNet     string // posted to the revenue/expense account
		wantTax     string // posted to the tax account, or 0 when folded in
	}{
		{"inclusive with a tax account posts the net", models.LineAmountTypesInclusive, "60", "60", true, "54", "6"},
		{"inclusive without one keeps the gross in the line", models.LineAmountTypesInclusive, "60", "60", false, "60", "0"},
		{"exclusive with a tax account posts the line as-is", models.LineAmountTypesExclusive, "54", "60", true, "54", "6"},
		{"exclusive without one folds the tax in", models.LineAmountTypesExclusive, "54", "60", false, "60", "0"},
		// A NoTax document posts no tax even though the client sent one: the
		// line stands as it is and there is no tax leg to unbalance it.
		{"no tax with a tax account ignores the line tax", models.LineAmountTypesNoTax, "60", "60", true, "60", "0"},
		{"no tax without one ignores it too", models.LineAmountTypesNoTax, "60", "60", false, "60", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			net, storedTax := documentLineNet(tc.amountTypes, decimal.RequireFromString(tc.lineAmount), tax, tc.taxAccOK)
			assert.Equal(t, tc.wantNet, net.String(), "line net")
			assert.Equal(t, tc.wantTax, storedTax.String(), "stored tax")

			// The sign convention a sale uses: control line is the total, every
			// other line is negated. The tax leg only exists when it is not
			// folded into the line.
			sum := decimal.RequireFromString(tc.total).Sub(net)
			if !storedTax.IsZero() {
				sum = sum.Sub(storedTax)
			}
			assert.Truef(t, sum.IsZero(), "journal does not balance: %s", sum)
		})
	}
}

// The tax a document posts is zero for NoTax and the sum of its lines
// otherwise — the figure the posting switch turns into the tax leg.
func TestDocumentTaxTotal(t *testing.T) {
	lines := []models.LineItem{
		{TaxAmount: decimal.RequireFromString("6")},
		{TaxAmount: decimal.RequireFromString("9")},
	}
	assert.Equal(t, "15", documentTaxTotal(models.LineAmountTypesExclusive, lines).String())
	assert.Equal(t, "15", documentTaxTotal(models.LineAmountTypesInclusive, lines).String())
	assert.True(t, documentTaxTotal(models.LineAmountTypesNoTax, lines).IsZero(),
		"a NoTax document posts no tax at all")
	assert.True(t, documentTaxTotal(models.LineAmountTypesExclusive, nil).IsZero())
}
