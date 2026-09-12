package bankfeed

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// plaidWebhookMaxAge is how old a signed delivery may be before it is treated as
// a replay. Plaid signs each webhook with an `iat` and expects a receiver to
// refuse anything older than five minutes.
const plaidWebhookMaxAge = 5 * time.Minute

// plaidKeyCacheMax bounds the signature key cache. It is a memory bound, not a
// rate bound: what limits the calls this endpoint makes is remembering the keys
// Plaid refused, so that a stranger posting the same made-up `kid` over and over
// costs one lookup rather than one per request.
const plaidKeyCacheMax = 32

// plaidKeyTTL is how long one answer about a signing key is trusted, whether it
// named a key or refused one. Keys rotate rarely, so an hour is ample; the expiry
// is what stops a refusal being remembered forever, and what stops a cache full
// of one day's keys from being tomorrow's.
const plaidKeyTTL = time.Hour

// plaidKeyLookupBurst and plaidKeyLookupWindow bound how often an unrecognised
// key id can send us to Plaid.
//
// The cache already makes one invented `kid` cost a single round trip however
// often it is repeated. What it cannot bound is a stream of *different* invented
// ids, each a fresh miss — that is public traffic, and every call it causes
// carries our client credentials. So it is bounded per unit of time rather than
// per key, and generously: a delivery naming a key we have already seen never
// spends from here, so in practice the budget only ever meets a rotation (one
// fetch, then the cache answers that key's remaining deliveries) or a restart,
// which empties the cache.
const (
	plaidKeyLookupBurst  = 12
	plaidKeyLookupWindow = time.Minute
)

// ErrWebhookVerifyThrottled is what VerifyWebhook returns when the adapter has
// refused to look up another unrecognised signing key for now. It is not a
// verdict on the delivery — nothing about it was checked — so the handler
// answers 503 rather than 401, and Plaid retries the delivery instead of the
// rejection being recorded as a bad signature.
var ErrWebhookVerifyThrottled = errors.New("too many unknown signing keys")

// plaidWebhookKey is one cache entry: the key when Plaid knew it, or the reason
// it did not. Caching the refusals is the point — an unknown key id is the case
// this endpoint most needs to stop asking about, and a cache that only remembers
// successes never fills, so it never bounds anything.
type plaidWebhookKey struct {
	key       *ecdsa.PublicKey
	err       error
	expiresAt time.Time
}

// VerifyWebhook implements WebhookVerifier: it checks that a delivery really
// came from Plaid.
//
// The notification endpoint is the one route in the bank feed without a tenant
// header — the caller is Plaid, not a logged-in user — so the signature is the
// only thing standing between it and a stranger marking somebody's connection
// linked. Plaid signs each request with an ES256 JWS carrying the hash of the
// body it signed, which is what makes the check cover the payload and not just
// the sender:
//
//  1. read `kid` out of the JWT header and fetch that public key from Plaid
//  2. verify the signature over `header.payload`
//  3. refuse a token issued more than five minutes ago
//  4. hash the raw body and compare it, in constant time, with the claim
//
// Step 4 is the one that is easy to get wrong. The hash covers the bytes exactly
// as they arrived, so the body must reach this function unmodified — which is
// why the handler verifies before it parses.
func (p *Plaid) VerifyWebhook(ctx context.Context, header string, body []byte) error {
	token := strings.TrimSpace(header)
	if token == "" {
		return errors.New("plaid: webhook carries no Plaid-Verification header")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("plaid: verification JWT is not a signed token")
	}

	var head struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("plaid: verification JWT header is not base64url: %w", err)
	}
	if err := json.Unmarshal(decoded, &head); err != nil {
		return fmt.Errorf("plaid: verification JWT header is not JSON: %w", err)
	}
	if head.Alg != "ES256" {
		return fmt.Errorf("plaid: verification JWT signed with %q, want ES256", head.Alg)
	}
	if head.Kid == "" {
		return errors.New("plaid: verification JWT names no key")
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("plaid: verification JWT signature is not base64url: %w", err)
	}
	if len(signature) != 64 {
		return fmt.Errorf("plaid: verification JWT signature is %d bytes, want 64", len(signature))
	}
	key, err := p.webhookKey(ctx, head.Kid)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	// A JWS ES256 signature is the fixed-width pair (r, s), each half the size
	// of the curve order.
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(key, digest[:], r, s) {
		return errors.New("plaid: webhook signature does not match any Plaid key")
	}

	var claims struct {
		Iat               int64  `json:"iat"`
		RequestBodySHA256 string `json:"request_body_sha256"`
	}
	decoded, err = base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("plaid: verification JWT claims are not base64url: %w", err)
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return fmt.Errorf("plaid: verification JWT claims are not JSON: %w", err)
	}
	if claims.Iat == 0 {
		return errors.New("plaid: verification JWT has no issued-at time")
	}
	// Signed, but signed a long time ago: the signature stays valid forever, so
	// the age is what stops a captured delivery being replayed. A future-dated
	// token is refused too — it is either a broken clock or an attempt to widen
	// the window.
	if age := time.Since(time.Unix(claims.Iat, 0)); age > plaidWebhookMaxAge || age < -plaidWebhookMaxAge {
		return fmt.Errorf("plaid: webhook was signed %s ago", age.Round(time.Second))
	}
	sum := sha256.Sum256(body)
	if subtle.ConstantTimeCompare([]byte(claims.RequestBodySHA256), []byte(hex.EncodeToString(sum[:]))) != 1 {
		return errors.New("plaid: webhook body does not match the signed digest")
	}
	return nil
}

// webhookKey returns the public key for a `kid`, asking Plaid the first time it
// is seen and remembering the answer either way. Keys turn over rarely and every
// delivery names one, so without the cache each notification would cost an extra
// round trip — and, because the endpoint is public, so would each request from
// anyone who cared to name a key that does not exist.
func (p *Plaid) webhookKey(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	if entry, ok := p.cachedWebhookKey(kid); ok {
		return entry.key, entry.err
	}
	if !p.spendKeyLookup() {
		return nil, fmt.Errorf("plaid: signing key %s not looked up: %w", kid, ErrWebhookVerifyThrottled)
	}
	key, err := p.fetchWebhookKey(ctx, kid)
	if err == nil || plaidRefusedKey(err) {
		p.rememberWebhookKey(kid, key, err)
	}
	return key, err
}

// spendKeyLookup takes one unit of the unknown-key budget, reporting whether any
// was left. Only a miss comes through here, so a key Plaid really signs with is
// never delayed by it.
func (p *Plaid) spendKeyLookup() bool {
	p.keyMu.Lock()
	defer p.keyMu.Unlock()
	now := time.Now()
	if now.Sub(p.keyLookupStart) >= plaidKeyLookupWindow {
		p.keyLookupStart = now
		p.keyLookupCount = 0
	}
	if p.keyLookupCount >= plaidKeyLookupBurst {
		return false
	}
	p.keyLookupCount++
	return true
}

// plaidRefusedKey reports whether a failed lookup is Plaid's answer rather than
// our inability to reach it. A refusal is worth remembering — asking again would
// get the same answer — while a timeout, an outage or a dropped connection says
// nothing about the key at all. Remembering one of those would turn a moment's
// network trouble into an hour of refusing deliveries that were genuine, which
// is the one way this cache can lose a real webhook.
func plaidRefusedKey(err error) bool {
	var apiErr *PlaidError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500
}

// cachedWebhookKey returns what is remembered about kid, discarding it first if
// it has gone stale.
func (p *Plaid) cachedWebhookKey(kid string) (plaidWebhookKey, bool) {
	p.keyMu.Lock()
	defer p.keyMu.Unlock()
	entry, ok := p.keys[kid]
	if !ok {
		return plaidWebhookKey{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(p.keys, kid)
		return plaidWebhookKey{}, false
	}
	return entry, true
}

// rememberWebhookKey stores an answer, making room by dropping whichever entry is
// closest to expiry. Refusing to store when the cache is full would be the wrong
// trade: every entry is only a shortcut to a call that can be made again, so
// evicting one costs a round trip, while refusing would leave an unknown key
// unanswered for the life of the process — and a key Plaid rotates in would go
// unrecognised, which reads as every delivery failing its signature check.
func (p *Plaid) rememberWebhookKey(kid string, key *ecdsa.PublicKey, err error) {
	p.keyMu.Lock()
	defer p.keyMu.Unlock()
	if _, exists := p.keys[kid]; !exists && len(p.keys) >= plaidKeyCacheMax {
		var oldest string
		var oldestExpiry time.Time
		for k, entry := range p.keys {
			if oldest == "" || entry.expiresAt.Before(oldestExpiry) {
				oldest, oldestExpiry = k, entry.expiresAt
			}
		}
		delete(p.keys, oldest)
	}
	p.keys[kid] = plaidWebhookKey{key: key, err: err, expiresAt: time.Now().Add(plaidKeyTTL)}
}

// fetchWebhookKey asks Plaid for the key and checks that it is one we can verify
// against before it is allowed anywhere near a signature check.
func (p *Plaid) fetchWebhookKey(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	var out struct {
		Key struct {
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"key"`
	}
	if err := p.do(ctx, "/webhook_verification_key/get", map[string]any{"key_id": kid}, &out); err != nil {
		return nil, err
	}
	if out.Key.Kty != "EC" || out.Key.Crv != "P-256" {
		return nil, fmt.Errorf("plaid: signing key %s is %s/%s, want EC/P-256", kid, out.Key.Kty, out.Key.Crv)
	}
	x, err := base64.RawURLEncoding.DecodeString(out.Key.X)
	if err != nil {
		return nil, fmt.Errorf("plaid: signing key %s has an unreadable x: %w", kid, err)
	}
	y, err := base64.RawURLEncoding.DecodeString(out.Key.Y)
	if err != nil {
		return nil, fmt.Errorf("plaid: signing key %s has an unreadable y: %w", kid, err)
	}
	// A JWK carries the point as two fixed-width coordinates (RFC 7518 §6.2.1.2),
	// so anything else is malformed rather than merely short. Checking the width
	// here is also what lets the uncompressed form below be built by copying.
	if len(x) != 32 || len(y) != 32 {
		return nil, fmt.Errorf("plaid: signing key %s has %d-byte coordinates, want 32", kid, len(x))
	}
	point := make([]byte, 65)
	point[0] = 4 // uncompressed SEC 1 point
	copy(point[1:33], x)
	copy(point[33:], y)
	// Parsing validates the point, so a key that is not on the curve is refused
	// before it can reach a signature check.
	parsed, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
	if err != nil {
		return nil, fmt.Errorf("plaid: signing key %s is not a point on P-256: %w", kid, err)
	}
	return parsed, nil
}
