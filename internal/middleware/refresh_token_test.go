package middleware

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRefreshToken_IsRandom(t *testing.T) {
	raw1, hash1, err := NewRefreshToken()
	require.NoError(t, err)
	raw2, hash2, err := NewRefreshToken()
	require.NoError(t, err)

	assert.NotEmpty(t, raw1)
	assert.NotEmpty(t, raw2)
	assert.NotEqual(t, raw1, raw2, "successive tokens must differ")
	assert.NotEqual(t, hash1, hash2, "hashes must differ for different tokens")
}

func TestHashRefreshToken_Deterministic(t *testing.T) {
	raw, hash, err := NewRefreshToken()
	require.NoError(t, err)

	assert.Equal(t, hash, HashRefreshToken(raw))
	assert.Equal(t, HashRefreshToken(raw), HashRefreshToken(raw))

	// Hash is valid sha256 hex → 64 characters, decodable as hex.
	assert.Len(t, hash, 64)
	_, err = hex.DecodeString(hash)
	assert.NoError(t, err)
}

func TestHashRefreshToken_DifferentInputs(t *testing.T) {
	assert.NotEqual(t, HashRefreshToken("a"), HashRefreshToken("b"))
}
