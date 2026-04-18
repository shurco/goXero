package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/testutil"
)

func TestIntegration_AccountCRUD(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	acc := &models.Account{
		Code:   "I-" + uuid.NewString()[:6],
		Name:   "Integration sales",
		Type:   "REVENUE",
		Status: "",
	}
	require.NoError(t, repos.Accounts.Create(ctx, seedDemoOrgID, acc))
	require.NotEqual(t, uuid.Nil, acc.AccountID)

	got, err := repos.Accounts.GetByID(ctx, seedDemoOrgID, acc.AccountID)
	require.NoError(t, err)
	assert.Equal(t, acc.Code, got.Code)
	assert.Equal(t, "ACTIVE", got.Status, "empty payload status defaults to ACTIVE in DB")

	got.Name = "Integration sales — updated"
	require.NoError(t, repos.Accounts.Update(ctx, seedDemoOrgID, got))

	list, err := repos.Accounts.List(ctx, seedDemoOrgID, AccountFilter{
		Status: "ACTIVE", Type: "REVENUE", Search: got.Code,
	})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Integration sales — updated", list[0].Name)

	require.NoError(t, repos.Accounts.Delete(ctx, seedDemoOrgID, acc.AccountID))

	after, err := repos.Accounts.GetByID(ctx, seedDemoOrgID, acc.AccountID)
	require.NoError(t, err)
	assert.Equal(t, "ARCHIVED", after.Status)
}

func TestIntegration_AccountDelete_NotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	err := repos.Accounts.Delete(context.Background(), seedDemoOrgID, uuid.New())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestIntegration_TaxRateCreateAndList(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	tr := &models.TaxRate{
		Name:           "GST 10 " + uuid.NewString()[:6],
		TaxType:        "OUTPUT",
		DisplayTaxRate: decimal.NewFromInt(10),
		EffectiveRate:  decimal.NewFromInt(10),
	}
	require.NoError(t, repos.TaxRates.Create(ctx, seedDemoOrgID, tr))
	require.NotEqual(t, uuid.Nil, tr.TaxRateID)

	list, err := repos.TaxRates.List(ctx, seedDemoOrgID)
	require.NoError(t, err)
	found := false
	for _, x := range list {
		if x.TaxRateID == tr.TaxRateID {
			found = true
			assert.Equal(t, "OUTPUT", x.TaxType)
		}
	}
	assert.True(t, found, "created tax rate must appear in List")
}
