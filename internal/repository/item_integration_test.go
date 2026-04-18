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

func TestIntegration_ItemCRUD(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	salesPrice := dec("49.95")
	it := &models.Item{
		Code:        "ITEM-" + uuid.NewString()[:6],
		Name:        "Integration widget",
		Description: "Widget for integration test",
		IsSold:      true,
		SalesDetails: &models.ItemPriceDetails{
			UnitPrice:   &salesPrice,
			AccountCode: "200",
			TaxType:     "NONE",
		},
	}
	require.NoError(t, repos.Items.Create(ctx, seedDemoOrgID, it))
	require.NotEqual(t, uuid.Nil, it.ItemID)

	got, err := repos.Items.GetByID(ctx, seedDemoOrgID, it.ItemID)
	require.NoError(t, err)
	assert.Equal(t, it.Code, got.Code)
	require.NotNil(t, got.SalesDetails)
	require.NotNil(t, got.SalesDetails.UnitPrice)
	assert.True(t, got.SalesDetails.UnitPrice.Equal(decimal.RequireFromString("49.95")))

	list, err := repos.Items.List(ctx, seedDemoOrgID)
	require.NoError(t, err)
	assert.NotEmpty(t, list)

	require.NoError(t, repos.Items.Delete(ctx, seedDemoOrgID, it.ItemID))

	_, err = repos.Items.GetByID(ctx, seedDemoOrgID, it.ItemID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestIntegration_ItemDelete_NotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	err := repos.Items.Delete(context.Background(), seedDemoOrgID, uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}
