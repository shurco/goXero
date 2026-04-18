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

func buildInvoice(t *testing.T) *models.Invoice {
	t.Helper()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	return &models.Invoice{
		Type:            models.InvoiceTypeAccRec,
		Status:          models.InvoiceStatusDraft,
		LineAmountTypes: models.LineAmountTypesExclusive,
		CurrencyCode:    "USD",
		Date:            &now,
		DueDate:         &now,
		InvoiceNumber:   "I-" + uuid.NewString()[:6],
		LineItems: []models.LineItem{
			{
				Description: "widget",
				Quantity:    dec("2"),
				UnitAmount:  dec("25"),
				TaxAmount:   dec("5"),
				AccountCode: "200",
			},
		},
	}
}

func TestIntegration_InvoiceUpdateStatusAndSummary(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	inv := buildInvoice(t)
	require.NoError(t, repos.Invoices.Create(ctx, seedDemoOrgID, inv))

	require.NoError(t, repos.Invoices.UpdateStatus(ctx, seedDemoOrgID, inv.InvoiceID, models.InvoiceStatusAuthorised))
	got, err := repos.Invoices.GetByID(ctx, seedDemoOrgID, inv.InvoiceID)
	require.NoError(t, err)
	assert.Equal(t, models.InvoiceStatusAuthorised, got.Status)

	summary, err := repos.Invoices.Summary(ctx, seedDemoOrgID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, summary.TotalInvoices, 1)
	assert.GreaterOrEqual(t, summary.Authorised, 1)
	assert.True(t, summary.TotalDue.GreaterThanOrEqual(decimal.Zero))
}

func TestIntegration_InvoiceUpdateStatus_NotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	err := repos.Invoices.UpdateStatus(context.Background(), seedDemoOrgID, uuid.New(), models.InvoiceStatusPaid)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestIntegration_InvoiceListFilters(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	inv := buildInvoice(t)
	require.NoError(t, repos.Invoices.Create(ctx, seedDemoOrgID, inv))

	list, total, err := repos.Invoices.List(ctx, seedDemoOrgID,
		InvoiceFilter{Type: models.InvoiceTypeAccRec, Status: models.InvoiceStatusDraft, Search: inv.InvoiceNumber},
		models.Pagination{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.NotEmpty(t, list)
}

func TestIntegration_PaymentList(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	inv := buildInvoice(t)
	require.NoError(t, repos.Invoices.Create(ctx, seedDemoOrgID, inv))
	require.NoError(t, repos.Invoices.UpdateStatus(ctx, seedDemoOrgID, inv.InvoiceID, models.InvoiceStatusAuthorised))

	invID := inv.InvoiceID
	require.NoError(t, repos.Payments.Create(ctx, seedDemoOrgID, &models.Payment{
		InvoiceID:   &invID,
		Amount:      dec("10"),
		Date:        time.Now().UTC(),
		PaymentType: "ACCRECPAYMENT",
		Status:      "AUTHORISED",
		Reference:   "ref-" + uuid.NewString()[:4],
	}))

	list, total, err := repos.Payments.List(ctx, seedDemoOrgID,
		models.Pagination{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.NotEmpty(t, list)
}
