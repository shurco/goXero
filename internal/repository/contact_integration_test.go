package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/models"
	"github.com/shurco/goxero/internal/testutil"
)

func TestIntegration_ContactCRUD(t *testing.T) {
	t.Parallel()

	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	c := &models.Contact{
		Name:          "Acme Widgets " + uuid.NewString()[:6],
		ContactStatus: "ACTIVE",
		IsCustomer:    true,
		EmailAddress:  "sales@acme.example",
	}
	require.NoError(t, repos.Contacts.Create(ctx, seedDemoOrgID, c))
	require.NotEqual(t, uuid.Nil, c.ContactID)

	got, err := repos.Contacts.GetByID(ctx, seedDemoOrgID, c.ContactID)
	require.NoError(t, err)
	assert.Equal(t, c.Name, got.Name)

	got.Name = got.Name + " (Updated)"
	got.IsSupplier = true
	require.NoError(t, repos.Contacts.Update(ctx, seedDemoOrgID, got))

	isCustomer, isSupplier := true, true
	list, _, err := repos.Contacts.List(ctx, seedDemoOrgID, ContactFilter{
		Status:     "ACTIVE",
		Search:     "Acme Widgets",
		IsCustomer: &isCustomer,
		IsSupplier: &isSupplier,
	}, models.Pagination{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.NotEmpty(t, list)

	require.NoError(t, repos.Contacts.Archive(ctx, seedDemoOrgID, c.ContactID))
	after, err := repos.Contacts.GetByID(ctx, seedDemoOrgID, c.ContactID)
	require.NoError(t, err)
	assert.Equal(t, "ARCHIVED", after.ContactStatus)
}

func TestIntegration_ContactArchive_NotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	err := repos.Contacts.Archive(context.Background(), seedDemoOrgID, uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}
