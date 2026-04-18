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

func TestIntegration_OrganisationCreateAndLookup(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	o := &models.Organisation{
		Name:         "Int-" + uuid.NewString()[:6],
		BaseCurrency: "USD",
	}
	require.NoError(t, repos.Organisations.Create(ctx, o))
	require.NotEqual(t, uuid.Nil, o.OrganisationID)
	require.NotEmpty(t, o.APIKey)

	byID, err := repos.Organisations.GetByID(ctx, o.OrganisationID)
	require.NoError(t, err)
	assert.Equal(t, o.Name, byID.Name)
}

func TestIntegration_OrganisationGetByID_NotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	_, err := repos.Organisations.GetByID(context.Background(), uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}
