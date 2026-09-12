package bankfeed

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSecretBoxRoundTrip pins the property the Plaid adapter depends on: an
// access token survives a seal/open cycle byte for byte, and the stored blob is
// not the plaintext.
func TestSecretBoxRoundTrip(t *testing.T) {
	t.Parallel()
	box, err := NewSecretBox("correct horse battery staple")
	require.NoError(t, err)

	const token = "access-production-2f1c9d1e-4a4d-4ec1-9b1e-0d7d1c1b8f77"
	sealed, err := box.Seal(token)
	require.NoError(t, err)
	require.NotEmpty(t, sealed)
	assert.NotContains(t, string(sealed), token, "ciphertext must not carry the plaintext")

	// A second seal of the same token must differ — the nonce is random, so two
	// connections sharing a token do not share a blob.
	again, err := box.Seal(token)
	require.NoError(t, err)
	assert.False(t, bytes.Equal(sealed, again), "each seal must use a fresh nonce")

	got, err := box.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, token, got)
}

// TestSecretBoxEmptyIsNil documents the contract that keeps the column NULL for
// providers which issue no per-connection secret (GoCardless): sealing nothing
// yields nothing, and opening nothing yields the empty string.
func TestSecretBoxEmptyIsNil(t *testing.T) {
	t.Parallel()
	box, err := NewSecretBox("k")
	require.NoError(t, err)

	sealed, err := box.Seal("")
	require.NoError(t, err)
	assert.Nil(t, sealed)

	plain, err := box.Open(nil)
	require.NoError(t, err)
	assert.Empty(t, plain)
}

// TestSecretBoxRejectsWrongKey covers the failure mode operators actually hit:
// BANKFEED_ENCRYPTION_KEY rotated away, leaving stored tokens undecryptable.
func TestSecretBoxRejectsWrongKey(t *testing.T) {
	t.Parallel()
	sealer, err := NewSecretBox("key-one")
	require.NoError(t, err)
	sealed, err := sealer.Seal("item-access-token")
	require.NoError(t, err)

	other, err := NewSecretBox("key-two")
	require.NoError(t, err)
	_, err = other.Open(sealed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BANKFEED_ENCRYPTION_KEY")
}

// TestSecretBoxRejectsTamperedCiphertext — GCM is authenticated, so a flipped
// bit anywhere in the blob must fail rather than decrypt to garbage.
func TestSecretBoxRejectsTamperedCiphertext(t *testing.T) {
	t.Parallel()
	box, err := NewSecretBox("k")
	require.NoError(t, err)
	sealed, err := box.Seal("item-access-token")
	require.NoError(t, err)

	sealed[len(sealed)-1] ^= 0xff
	_, err = box.Open(sealed)
	require.Error(t, err)

	// Truncation below the nonce length must not panic on the slice.
	_, err = box.Open(sealed[:3])
	require.Error(t, err)
}

// TestNewSecretBoxRequiresKey — an unset env var must not silently produce a
// well-known key, because that would look like working encryption.
func TestNewSecretBoxRequiresKey(t *testing.T) {
	t.Parallel()
	_, err := NewSecretBox("")
	require.ErrorIs(t, err, ErrNoSecretBox)
}
