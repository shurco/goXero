package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/testutil"
)

func TestIntegration_UserGetByEmailAndID(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	email := "u-" + uuid.NewString()[:6] + "@example.com"
	u, err := repos.Users.Create(ctx, email, "hash", "Al", "Ice")
	require.NoError(t, err)

	byEmail, err := repos.Users.GetByEmail(ctx, email)
	require.NoError(t, err)
	assert.Equal(t, u.UserID, byEmail.UserID)
	assert.Equal(t, "Al", byEmail.FirstName)
	assert.Equal(t, "Ice", byEmail.LastName)

	byID, err := repos.Users.GetByID(ctx, u.UserID)
	require.NoError(t, err)
	assert.Equal(t, email, byID.Email)
}

func TestIntegration_UserGetByEmail_NotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	_, err := repos.Users.GetByEmail(context.Background(), "nope@example.com")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestIntegration_UserGetByID_NotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	_, err := repos.Users.GetByID(context.Background(), uuid.New())
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestIntegration_LinkOrganisation_Idempotent(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	email := "link-" + uuid.NewString()[:6] + "@example.com"
	u, err := repos.Users.Create(ctx, email, "hash", "L", "U")
	require.NoError(t, err)

	require.NoError(t, repos.Users.LinkOrganisation(ctx, seedDemoOrgID, u.UserID, "admin"))
	require.NoError(t, repos.Users.LinkOrganisation(ctx, seedDemoOrgID, u.UserID, "admin"),
		"repeated link must not fail (ON CONFLICT DO NOTHING)")

	ok, err := repos.Users.HasOrganisationAccess(ctx, u.UserID, seedDemoOrgID)
	require.NoError(t, err)
	assert.True(t, ok)
}
