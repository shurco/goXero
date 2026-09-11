package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/testutil"
)

// The rates of the capture's chart: Tax on Purchases is 8.25%, Tax on Goods
// 8.75%, Tax Exempt 0%.
var (
	input8_25  = dec("8.2500")
	output8_75 = dec("8.7500")
)

// A line whose amount is the net owes the rate on top of it. The capture's own
// bank lines are in this shape: 11.09 of travel plus 0.91 of Tax on Purchases
// is the 12.00 that left the account.
func TestLineTaxExclusive(t *testing.T) {
	t.Parallel()

	cases := []struct{ amount, want string }{
		{"11.09", "0.91"},
		{"5.54", "0.46"},
		{"101.62", "8.38"},
		{"-11.09", "-0.91"},
		{"1000.00", "82.50"},
	}
	for _, c := range cases {
		got := lineTax(models.LineAmountTypesExclusive, dec(c.amount), input8_25)
		assert.True(t, dec(c.want).Equal(got),
			"8.25%% of %s should be %s, got %s", c.amount, c.want, got)
	}
}

// A line whose amount already contains its tax takes the tax out of that
// amount instead, so the net and the tax always come back to the amount the
// line states — however the division rounds.
func TestLineTaxInclusiveTakesTheTaxOutOfTheAmount(t *testing.T) {
	t.Parallel()

	cases := []struct{ amount, wantTax string }{
		{"15.50", "1.18"}, // 15.50 / 1.0825 = 14.32 net
		{"12.00", "0.91"}, // 12.00 / 1.0825 = 11.09 net
		{"6.00", "0.46"},
		{"-6.00", "-0.46"},
		{"0.03", "0.00"}, // 0.0277 of net, under half a cent of tax
	}
	for _, c := range cases {
		amount := dec(c.amount)
		tax := lineTax(models.LineAmountTypesInclusive, amount, input8_25)
		assert.True(t, dec(c.wantTax).Equal(tax),
			"the tax inside %s should be %s, got %s", c.amount, c.wantTax, tax)

		net := amount.Sub(tax)
		assert.True(t, net.Add(tax).Equal(amount),
			"the net %s and the tax %s should come back to %s", net, tax, amount)
		assert.True(t, net.Equal(net.Round(moneyPlaces)),
			"the net %s should be a money figure", net)
	}
}

// A rate of nothing is a tax of nothing, on either kind of line, and an
// amount the rate cannot be read from is not invented.
func TestLineTaxOfAZeroRatedLineIsZero(t *testing.T) {
	t.Parallel()

	for _, lat := range []string{models.LineAmountTypesExclusive, models.LineAmountTypesInclusive} {
		assert.True(t, lineTax(lat, dec("15.50"), decimal.Zero).IsZero())
	}
}

func TestLineTaxRoundsToMoney(t *testing.T) {
	t.Parallel()

	// 0.07 of goods at 8.75% is 0.6125 cents — half a cent, so 0.01.
	assert.True(t, dec("0.01").Equal(lineTax(models.LineAmountTypesExclusive, dec("0.07"), output8_75)))
	// 0.05 of goods at 8.75% is 0.4375 of a cent — under half, so nothing.
	assert.True(t, lineTax(models.LineAmountTypesExclusive, dec("0.05"), output8_75).IsZero())
}

// The totals a bank transaction carries have to say the same thing as its
// lines: for an inclusive transaction the subtotal is the total less the tax.
func TestRecalculateBankTxSplitsAnInclusiveTotal(t *testing.T) {
	t.Parallel()

	bt := &models.BankTransaction{
		LineAmountTypes: models.LineAmountTypesInclusive,
		LineItems: []models.LineItem{
			{Quantity: decimal.NewFromInt(1), UnitAmount: dec("15.50"), TaxAmount: dec("1.18")},
		},
	}
	recalculateBankTx(bt)
	assert.True(t, dec("15.50").Equal(bt.Total), "total %s", bt.Total)
	assert.True(t, dec("1.18").Equal(bt.TotalTax), "tax %s", bt.TotalTax)
	assert.True(t, dec("14.32").Equal(bt.SubTotal), "subtotal %s", bt.SubTotal)
	assert.True(t, bt.SubTotal.Add(bt.TotalTax).Equal(bt.Total),
		"%s + %s should be %s", bt.SubTotal, bt.TotalTax, bt.Total)
}

// End to end: a line that names a rate and nothing else reaches the ledger
// with the tax taken out of it and posted to the tax control account, and the
// expense carries only the net. This is what the reconcile screen does — it
// sends TaxType and has no field for the amount.
func TestIntegration_CreateDerivesTheTaxOfALineThatNamesOnlyARate(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	var bankAccountID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT account_id FROM accounts WHERE organisation_id=$1 AND code='090'`,
		seedDemoOrgID).Scan(&bankAccountID))

	now := time.Now().UTC().Truncate(24 * time.Hour)
	bt := &models.BankTransaction{
		Type:            models.BankTransactionTypeSpend,
		BankAccountID:   &bankAccountID,
		IsReconciled:    true,
		Date:            &now,
		Reference:       "INT-TAX-" + uuid.NewString()[:8],
		CurrencyCode:    "USD",
		Status:          "AUTHORISED",
		LineAmountTypes: models.LineAmountTypesInclusive,
		LineItems: []models.LineItem{{
			Description: "A purchase of 15.50, tax inclusive",
			Quantity:    decimal.NewFromInt(1),
			UnitAmount:  dec("15.50"),
			AccountCode: "453",
			TaxType:     "INPUT",
		}},
	}
	require.NoError(t, repos.BankTransactions.Create(ctx, seedDemoOrgID, bt))

	require.Len(t, bt.LineItems, 1)
	assert.True(t, dec("1.18").Equal(bt.LineItems[0].TaxAmount),
		"the line's tax should be 1.18, got %s", bt.LineItems[0].TaxAmount)
	assert.True(t, dec("14.32").Equal(bt.SubTotal), "subtotal %s", bt.SubTotal)
	assert.True(t, dec("15.50").Equal(bt.Total), "total %s", bt.Total)

	rows, err := pool.Query(ctx, `
		SELECT a.code, l.net_amount, l.tax_amount, COALESCE(l.tax_type,'')
		  FROM gl_journals j
		  JOIN gl_journal_lines l ON l.journal_id = j.journal_id
		  JOIN accounts a ON a.account_id = l.account_id
		 WHERE j.organisation_id=$1 AND j.source_type='BANKTRANSACTION' AND j.source_id=$2
		 ORDER BY a.code`, seedDemoOrgID, bt.BankTransactionID)
	require.NoError(t, err)
	defer rows.Close()

	type line struct {
		code     string
		net, tax decimal.Decimal
		taxType  string
	}
	var got []line
	for rows.Next() {
		var l line
		require.NoError(t, rows.Scan(&l.code, &l.net, &l.tax, &l.taxType))
		got = append(got, l)
	}
	require.NoError(t, rows.Err())

	// The bank is credited the whole 15.50, the expense is debited the net of
	// 14.32, and the tax control account is debited the 1.18 — so the journal
	// still balances and the ledger records the tax the line names.
	byCode := map[string]line{}
	sum := decimal.Zero
	for _, l := range got {
		byCode[l.code] = l
		sum = sum.Add(l.net)
	}
	require.Len(t, got, 3, "expected a bank, an expense and a tax line, got %v", got)
	assert.True(t, sum.IsZero(), "the journal should balance, sum %s", sum)

	assert.True(t, dec("-15.50").Equal(byCode["090"].net),
		"the bank should be credited 15.50, got %s", byCode["090"].net)
	assert.True(t, dec("14.32").Equal(byCode["453"].net),
		"the expense should be debited the net 14.32, got %s", byCode["453"].net)
	assert.Equal(t, "INPUT", byCode["453"].taxType)
	assert.True(t, dec("1.18").Equal(byCode["820"].net),
		"tax control should be debited 1.18, got %s", byCode["820"].net)
}

// The same derivation on a line whose amount is the net: the tax goes on top of
// it, the bank still moves the sum the caller stated, and the journal balances.
// This is the shape the imported bank lines are in.
func TestIntegration_CreateAddsTheTaxToAnExclusiveLine(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	var bankAccountID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT account_id FROM accounts WHERE organisation_id=$1 AND code='090'`,
		seedDemoOrgID).Scan(&bankAccountID))

	now := time.Now().UTC().Truncate(24 * time.Hour)
	bt := &models.BankTransaction{
		Type:            models.BankTransactionTypeSpend,
		BankAccountID:   &bankAccountID,
		IsReconciled:    true,
		Date:            &now,
		Reference:       "INT-TAX-EX-" + uuid.NewString()[:8],
		CurrencyCode:    "USD",
		Status:          "AUTHORISED",
		LineAmountTypes: models.LineAmountTypesExclusive,
		LineItems: []models.LineItem{{
			Description: "A purchase of 11.09, tax on top",
			Quantity:    decimal.NewFromInt(1),
			UnitAmount:  dec("11.09"),
			AccountCode: "493",
			TaxType:     "INPUT",
		}},
	}
	require.NoError(t, repos.BankTransactions.Create(ctx, seedDemoOrgID, bt))

	require.Len(t, bt.LineItems, 1)
	assert.True(t, dec("11.09").Equal(bt.SubTotal), "subtotal %s", bt.SubTotal)
	assert.True(t, dec("0.91").Equal(bt.TotalTax), "tax %s", bt.TotalTax)
	assert.True(t, dec("12.00").Equal(bt.Total), "total %s", bt.Total)

	rows, err := pool.Query(ctx, `
		SELECT a.code, l.net_amount
		  FROM gl_journals j
		  JOIN gl_journal_lines l ON l.journal_id = j.journal_id
		  JOIN accounts a ON a.account_id = l.account_id
		 WHERE j.organisation_id=$1 AND j.source_type='BANKTRANSACTION' AND j.source_id=$2`,
		seedDemoOrgID, bt.BankTransactionID)
	require.NoError(t, err)
	defer rows.Close()

	net := map[string]decimal.Decimal{}
	sum := decimal.Zero
	for rows.Next() {
		var code string
		var amount decimal.Decimal
		require.NoError(t, rows.Scan(&code, &amount))
		net[code] = amount
		sum = sum.Add(amount)
	}
	require.NoError(t, rows.Err())
	require.Len(t, net, 3, "expected a bank, an expense and a tax line, got %v", net)
	assert.True(t, sum.IsZero(), "the journal should balance, sum %s", sum)
	assert.True(t, dec("-12.00").Equal(net["090"]), "the bank should be credited 12.00, got %s", net["090"])
	assert.True(t, dec("11.09").Equal(net["493"]), "the expense should be debited the net 11.09, got %s", net["493"])
	assert.True(t, dec("0.91").Equal(net["820"]), "tax control should be debited 0.91, got %s", net["820"])
}
