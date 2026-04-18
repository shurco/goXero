package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/testutil"
)

func newTestUser(t *testing.T, repos *Repositories) uuid.UUID {
	t.Helper()
	email := "rt-" + uuid.NewString()[:8] + "@example.com"
	u, err := repos.Users.Create(context.Background(), email, "hash", "R", "T")
	require.NoError(t, err)
	return u.UserID
}

// hashFor produces a deterministic but unique 64-character hex digest for
// test fixtures. Mirrors the sha256 format the production code stores.
func hashFor(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func TestIntegration_RefreshTokens_CreateAndGet(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	uid := newTestUser(t, repos)
	hash := hashFor("create-" + uuid.NewString())
	expires := time.Now().Add(time.Hour)

	rt, err := repos.RefreshTokens.Create(ctx, uid, hash, expires, "TestUA", "127.0.0.1")
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, rt.TokenID)

	got, err := repos.RefreshTokens.GetByHash(ctx, hash)
	require.NoError(t, err)
	assert.Equal(t, rt.TokenID, got.TokenID)
	assert.Equal(t, uid, got.UserID)
	assert.Nil(t, got.RevokedAt)
	assert.Equal(t, "TestUA", got.UserAgent)
	assert.Equal(t, "127.0.0.1", got.IP)
}

func TestIntegration_RefreshTokens_GetByHash_NotFound(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)

	_, err := repos.RefreshTokens.GetByHash(context.Background(), "deadbeef")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestIntegration_RefreshTokens_Rotate(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	uid := newTestUser(t, repos)
	oldHash := hashFor("rotate-old-" + uuid.NewString())
	old, err := repos.RefreshTokens.Create(ctx, uid, oldHash, time.Now().Add(time.Hour), "", "")
	require.NoError(t, err)

	newHash := hashFor("rotate-new-" + uuid.NewString())
	nw, err := repos.RefreshTokens.Rotate(ctx, old.TokenID, uid, newHash, time.Now().Add(time.Hour), "", "")
	require.NoError(t, err)
	assert.NotEqual(t, old.TokenID, nw.TokenID)

	// Old row must now be revoked and linked to the new one.
	got, err := repos.RefreshTokens.GetByHash(ctx, oldHash)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedAt, "old token must be revoked after rotation")
	require.NotNil(t, got.ReplacedByID)
	assert.Equal(t, nw.TokenID, *got.ReplacedByID)

	// Rotating the already-revoked token again must be refused so the
	// handler can treat reuse as an attack signal.
	_, err = repos.RefreshTokens.Rotate(ctx, old.TokenID, uid, hashFor("rotate-reuse"), time.Now().Add(time.Hour), "", "")
	assert.ErrorIs(t, err, ErrForbidden)
}

func TestIntegration_RefreshTokens_RevokeByIDAndAll(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	repos := New(pool)
	ctx := context.Background()

	uid := newTestUser(t, repos)
	h1 := hashFor("revoke-1-" + uuid.NewString())
	h2 := hashFor("revoke-2-" + uuid.NewString())
	t1, err := repos.RefreshTokens.Create(ctx, uid, h1, time.Now().Add(time.Hour), "", "")
	require.NoError(t, err)
	_, err = repos.RefreshTokens.Create(ctx, uid, h2, time.Now().Add(time.Hour), "", "")
	require.NoError(t, err)

	require.NoError(t, repos.RefreshTokens.RevokeByID(ctx, t1.TokenID, uid))
	got, err := repos.RefreshTokens.GetByHash(ctx, h1)
	require.NoError(t, err)
	assert.NotNil(t, got.RevokedAt)

	// Second token still live.
	got2, err := repos.RefreshTokens.GetByHash(ctx, h2)
	require.NoError(t, err)
	assert.Nil(t, got2.RevokedAt)

	// Revoke all → second token flipped too.
	require.NoError(t, repos.RefreshTokens.RevokeAllForUser(ctx, uid))
	got2, err = repos.RefreshTokens.GetByHash(ctx, h2)
	require.NoError(t, err)
	assert.NotNil(t, got2.RevokedAt)
}
