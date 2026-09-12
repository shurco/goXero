package testutil

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// SignPlaidWebhook builds the JWS Plaid sends: a signature over `header.payload`
// with the hash of the body in the claims. It lives here rather than beside one
// of its callers because both packages that need a signed delivery — the adapter
// testing its own verification, and the handler testing the route that uses it —
// have to produce bytes Plaid would produce, and two implementations of that
// would be two chances to drift apart from the real thing.
func SignPlaidWebhook(t *testing.T, key *ecdsa.PrivateKey, kid string, body []byte, iat time.Time, alg string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(
		`{"alg":"` + alg + `","kid":"` + kid + `","typ":"JWT"}`))
	sum := sha256.Sum256(body)
	claims, err := json.Marshal(map[string]any{
		"iat":                 iat.Unix(),
		"request_body_sha256": hex.EncodeToString(sum[:]),
	})
	require.NoError(t, err)
	signing := header + "." + base64.RawURLEncoding.EncodeToString(claims)

	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	require.NoError(t, err)
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// PlaidJWK renders a P-256 public key the way Plaid's key endpoint does: x and y
// as fixed-width base64url big-endian integers, under the kid the signature
// names.
func PlaidJWK(t *testing.T, key *ecdsa.PublicKey, kid string) string {
	t.Helper()
	pub, err := key.Bytes()
	require.NoError(t, err)
	require.Len(t, pub, 65, "uncompressed P-256 point")
	return fmt.Sprintf(`{"key":{"kty":"EC","crv":"P-256","alg":"ES256","use":"sig","kid":"%s","x":"%s","y":"%s"}}`,
		kid, base64.RawURLEncoding.EncodeToString(pub[1:33]), base64.RawURLEncoding.EncodeToString(pub[33:]))
}
