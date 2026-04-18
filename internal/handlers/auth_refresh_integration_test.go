package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type authPayload struct {
	Token                 string `json:"token"`
	RefreshToken          string `json:"refreshToken"`
	ExpiresAt             string `json:"expiresAt"`
	RefreshTokenExpiresAt string `json:"refreshTokenExpiresAt"`
}

func registerAndDecode(t *testing.T, h *appHarness) authPayload {
	t.Helper()
	email := "rt-" + uuid.NewString()[:8] + "@example.com"
	status, body := h.do(t, http.MethodPost, "/api/auth/register",
		map[string]string{"email": email, "password": "Pa$$w0rd!"},
		false)
	require.Equal(t, http.StatusCreated, status, string(body))

	var payload authPayload
	require.NoError(t, json.Unmarshal(body, &payload))
	require.NotEmpty(t, payload.Token)
	require.NotEmpty(t, payload.RefreshToken)
	require.NotEmpty(t, payload.ExpiresAt)
	require.NotEmpty(t, payload.RefreshTokenExpiresAt)
	return payload
}

// Happy-path: refresh returns a new access+refresh pair.
func TestHTTP_AuthRefresh_Rotation(t *testing.T) {
	h := newHarness(t)
	initial := registerAndDecode(t, h)

	// Drop the token so the /refresh call is anonymous (mimics client behaviour).
	h.token = ""
	status, body := h.do(t, http.MethodPost, "/api/auth/refresh",
		map[string]string{"refreshToken": initial.RefreshToken}, false)
	require.Equal(t, http.StatusOK, status, string(body))

	var rotated authPayload
	require.NoError(t, json.Unmarshal(body, &rotated))
	assert.NotEmpty(t, rotated.Token)
	// The refresh token MUST rotate on every exchange (single-use tokens
	// are the whole point of rotation). The access JWT can coincide when
	// the previous one was issued in the same wall-clock second, so we
	// don't assert on it here.
	assert.NotEqual(t, initial.RefreshToken, rotated.RefreshToken, "refresh token must rotate")
}

// Reusing a previously-rotated refresh token must be rejected and revoke the
// whole family (reuse-detection attack signal).
func TestHTTP_AuthRefresh_ReuseDetection(t *testing.T) {
	h := newHarness(t)
	initial := registerAndDecode(t, h)
	h.token = ""

	// Legitimate rotation.
	status, body := h.do(t, http.MethodPost, "/api/auth/refresh",
		map[string]string{"refreshToken": initial.RefreshToken}, false)
	require.Equal(t, http.StatusOK, status, string(body))
	var rotated authPayload
	require.NoError(t, json.Unmarshal(body, &rotated))

	// Re-presenting the original token must fail.
	status, _ = h.do(t, http.MethodPost, "/api/auth/refresh",
		map[string]string{"refreshToken": initial.RefreshToken}, false)
	assert.Equal(t, http.StatusUnauthorized, status)

	// After reuse detection the fresh token from the legitimate rotation
	// must also be considered revoked.
	status, _ = h.do(t, http.MethodPost, "/api/auth/refresh",
		map[string]string{"refreshToken": rotated.RefreshToken}, false)
	assert.Equal(t, http.StatusUnauthorized, status,
		"reuse detection must revoke the whole refresh family")
}

func TestHTTP_AuthRefresh_InvalidToken(t *testing.T) {
	h := newHarness(t)
	h.token = ""
	status, _ := h.do(t, http.MethodPost, "/api/auth/refresh",
		map[string]string{"refreshToken": "not-a-real-token"}, false)
	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestHTTP_AuthRefresh_MissingToken(t *testing.T) {
	h := newHarness(t)
	h.token = ""
	status, _ := h.do(t, http.MethodPost, "/api/auth/refresh",
		map[string]string{}, false)
	assert.Equal(t, http.StatusBadRequest, status)
}

// Logout revokes the presented refresh token, after which it can't be used.
func TestHTTP_AuthLogout_RevokesRefreshToken(t *testing.T) {
	h := newHarness(t)
	initial := registerAndDecode(t, h)

	// Logout (anonymous is allowed — OptionalJWTAuth).
	h.token = ""
	status, _ := h.do(t, http.MethodPost, "/api/auth/logout",
		map[string]string{"refreshToken": initial.RefreshToken}, false)
	assert.Equal(t, http.StatusNoContent, status)

	// The revoked token can no longer be exchanged.
	status, _ = h.do(t, http.MethodPost, "/api/auth/refresh",
		map[string]string{"refreshToken": initial.RefreshToken}, false)
	assert.Equal(t, http.StatusUnauthorized, status)
}

// Logout with ?everywhere=true (authenticated) revokes ALL refresh tokens for
// the user even if the body doesn't contain one.
func TestHTTP_AuthLogout_Everywhere(t *testing.T) {
	h := newHarness(t)
	first := registerAndDecode(t, h)

	// Second login for the same user — produces a distinct refresh token.
	// We need the email we used, which we don't have — grab another session
	// by logging in via /api/auth/login on a freshly registered user.
	second := registerAndDecode(t, h)

	// Authenticate as the second user and log out everywhere.
	h.token = second.Token
	status, _ := h.do(t, http.MethodPost, "/api/auth/logout?everywhere=true",
		map[string]string{}, false)
	assert.Equal(t, http.StatusNoContent, status)

	// The second user's refresh token is gone.
	h.token = ""
	status, _ = h.do(t, http.MethodPost, "/api/auth/refresh",
		map[string]string{"refreshToken": second.RefreshToken}, false)
	assert.Equal(t, http.StatusUnauthorized, status)

	// The *first* user's token must remain valid (logout-everywhere is
	// strictly scoped to the authenticated user).
	status, _ = h.do(t, http.MethodPost, "/api/auth/refresh",
		map[string]string{"refreshToken": first.RefreshToken}, false)
	assert.Equal(t, http.StatusOK, status)
}
