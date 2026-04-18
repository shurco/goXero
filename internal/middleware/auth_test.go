package middleware

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shurco/goxero/internal/config"
)

func testAuthCfg(t *testing.T) config.AuthConfig {
	t.Helper()
	return config.AuthConfig{
		JWTSecret:        "test-secret-32-chars-minimum-length-ok",
		AccessTokenTTL:   time.Minute,
		RefreshTokenTTL:  time.Hour,
		TenantHeaderName: "Xero-Tenant-Id",
	}
}

func TestIssueAndParseToken_RoundTrip(t *testing.T) {
	cfg := testAuthCfg(t)
	userID := uuid.New()

	tok, err := IssueToken(cfg, userID, "u@example.com")
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	claims, err := ParseToken(cfg, tok)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, "u@example.com", claims.Email)
}

func TestParseToken_RejectsWrongSecret(t *testing.T) {
	cfg := testAuthCfg(t)
	tok, err := IssueToken(cfg, uuid.New(), "u@example.com")
	require.NoError(t, err)

	bad := cfg
	bad.JWTSecret = "different-secret"
	_, err = ParseToken(bad, tok)
	require.Error(t, err)
}

func TestParseToken_RejectsExpired(t *testing.T) {
	cfg := testAuthCfg(t)
	cfg.AccessTokenTTL = -time.Second

	tok, err := IssueToken(cfg, uuid.New(), "u@example.com")
	require.NoError(t, err)

	_, err = ParseToken(cfg, tok)
	require.Error(t, err)
}

func TestParseToken_RejectsGarbage(t *testing.T) {
	cfg := testAuthCfg(t)
	_, err := ParseToken(cfg, "not-a-jwt")
	require.Error(t, err)
}
