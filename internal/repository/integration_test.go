package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/testutil"
)

// UUIDs from migrations/00009_seed_demo.sql (stable demo dataset).
var (
	seedDemoOrgID  = uuid.MustParse("6823b27b-c48f-4099-bb27-4202a4f496a2")
	seedDemoUserID = uuid.MustParse("e906a37e-41c0-4b9d-b374-a34052b3b7d1")
)

func TestIntegration_ListForUser_SeedDemo(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	orgs, err := repos.Organisations.ListForUser(ctx, seedDemoUserID)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "Demo Company (Global)", orgs[0].Name)
	assert.Equal(t, seedDemoOrgID, orgs[0].OrganisationID)
}

func TestIntegration_GetOrganisation_SeedDemo(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	o, err := repos.Organisations.GetByID(ctx, seedDemoOrgID)
	require.NoError(t, err)
	require.NotNil(t, o)
	assert.Equal(t, "DEMO", o.ShortCode)
}

func TestIntegration_PaymentOverpayClampsAmountDue(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(24 * time.Hour)
	inv := models.Invoice{
		Type:            models.InvoiceTypeAccRec,
		Status:          models.InvoiceStatusAuthorised,
		LineAmountTypes: models.LineAmountTypesExclusive,
		CurrencyCode:    "USD",
		Date:            &now,
		DueDate:         &now,
		InvoiceNumber:   "INT-PAY-" + uuid.NewString()[:8],
		LineItems: []models.LineItem{
			{
				Description: "integration line",
				Quantity:    dec("1"),
				UnitAmount:  dec("100"),
				TaxAmount:   decimal.Zero,
				AccountCode: "200",
			},
		},
	}

	require.NoError(t, repos.Invoices.Create(ctx, seedDemoOrgID, &inv))

	invID := inv.InvoiceID
	pay := models.Payment{
		InvoiceID:   &invID,
		PaymentType: "ACCRECPAYMENT",
		Status:      "AUTHORISED",
		Date:        now,
		Amount:      dec("999"),
	}
	require.NoError(t, repos.Payments.Create(ctx, seedDemoOrgID, &pay))

	after, err := repos.Invoices.GetByID(ctx, seedDemoOrgID, invID)
	require.NoError(t, err)
	assert.True(t, after.AmountDue.IsZero(), "amount_due must not go negative")
	assert.True(t, after.AmountPaid.Equal(dec("999")))
	assert.Equal(t, models.InvoiceStatusPaid, after.Status)
}

func TestIntegration_UserCreate_DuplicateEmailReturnsErrAlreadyExists(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	email := "dup-" + uuid.NewString()[:8] + "@example.com"
	_, err := repos.Users.Create(ctx, email, "hash", "Alice", "One")
	require.NoError(t, err)

	_, err = repos.Users.Create(ctx, email, "hash", "Alice", "Two")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAlreadyExists),
		"duplicate email must surface as ErrAlreadyExists, got %v", err)
}

func TestIntegration_HasOrganisationAccess(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	ok, err := repos.Users.HasOrganisationAccess(ctx, seedDemoUserID, seedDemoOrgID)
	require.NoError(t, err)
	assert.True(t, ok, "seed admin must belong to seed org")

	ok, err = repos.Users.HasOrganisationAccess(ctx, uuid.New(), seedDemoOrgID)
	require.NoError(t, err)
	assert.False(t, ok, "random user must NOT have access")
}

func TestIntegration_PaymentCreate_RejectsForeignInvoice(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	foreignOrg := uuid.New()
	foreignInv := uuid.New()
	pay := models.Payment{
		InvoiceID:   &foreignInv,
		PaymentType: "ACCRECPAYMENT",
		Status:      "AUTHORISED",
		Date:        time.Now().UTC(),
		Amount:      dec("10"),
	}
	err := repos.Payments.Create(ctx, foreignOrg, &pay)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound),
		"foreign invoice_id must be rejected with ErrNotFound, got %v", err)
}

func TestIntegration_InvoiceList_IncludesContactName(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	contact := models.Contact{
		Name:          "ACME Ltd",
		ContactStatus: "ACTIVE",
	}
	require.NoError(t, repos.Contacts.Create(ctx, seedDemoOrgID, &contact))

	now := time.Now().UTC().Truncate(24 * time.Hour)
	cid := contact.ContactID
	inv := models.Invoice{
		Type:            models.InvoiceTypeAccRec,
		Status:          models.InvoiceStatusDraft,
		LineAmountTypes: models.LineAmountTypesExclusive,
		CurrencyCode:    "USD",
		Date:            &now,
		DueDate:         &now,
		ContactID:       &cid,
		InvoiceNumber:   "INV-C-" + uuid.NewString()[:8],
		LineItems: []models.LineItem{
			{Description: "x", Quantity: dec("1"), UnitAmount: dec("10"), TaxAmount: decimal.Zero, AccountCode: "200"},
		},
	}
	require.NoError(t, repos.Invoices.Create(ctx, seedDemoOrgID, &inv))

	list, _, err := repos.Invoices.List(ctx, seedDemoOrgID,
		InvoiceFilter{Search: inv.InvoiceNumber}, models.Pagination{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NotNil(t, list[0].Contact)
	assert.Equal(t, "ACME Ltd", list[0].Contact.Name)
}
