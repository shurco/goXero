package repository

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fingerprint is what separates a re-imported file from genuinely new
// lines, so cosmetic differences between the same line must not produce two
// different keys.
func TestStatementFingerprintNormalisesCosmeticDifferences(t *testing.T) {
	posted := mustTime(t, "2026-01-15")
	key := StatementFingerprint(posted, decimal.RequireFromString("-42.5"), "coffee shop", "REF 1")

	assert.Equal(t, key,
		StatementFingerprint(posted, decimal.RequireFromString("-42.50"), "Coffee  Shop", "ref 1"),
		"case, spacing and trailing zeroes must not change the key")
	assert.NotEqual(t, key,
		StatementFingerprint(posted, decimal.RequireFromString("-42.51"), "coffee shop", "REF 1"),
		"a different amount must change the key")
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return ts
}
