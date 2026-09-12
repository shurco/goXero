package bankfeed

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// SecretBox seals the per-connection secrets providers hand back during consent
// (today: Plaid's Item access_token). Those tokens are bearer credentials for a
// user's bank data, so they are never stored in the clear — `bank_feed_connections`
// keeps only the ciphertext this type produces.
//
// The key comes from BANKFEED_ENCRYPTION_KEY. Operators are not asked to produce
// exactly 32 bytes of entropy by hand, so any passphrase is accepted and hashed
// down to an AES-256 key with SHA-256; that is a key-derivation shortcut, not a
// KDF — the env var is expected to hold a server secret, not a user password.
type SecretBox struct {
	aead cipher.AEAD
}

// ErrNoSecretBox is returned when a connection carries a sealed secret but the
// server was started without BANKFEED_ENCRYPTION_KEY. Rotating that variable
// away makes every stored token undecryptable by design, so we fail loudly.
var ErrNoSecretBox = errors.New("bankfeed: no encryption key configured (set BANKFEED_ENCRYPTION_KEY)")

// NewSecretBox derives a AES-256-GCM box from an arbitrary passphrase. An empty
// passphrase is rejected so a missing env var cannot silently produce a
// well-known key.
func NewSecretBox(passphrase string) (*SecretBox, error) {
	if passphrase == "" {
		return nil, ErrNoSecretBox
	}
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("bankfeed: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("bankfeed: gcm: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}

// Seal encrypts plaintext, returning nonce||ciphertext. An empty plaintext
// yields nil so the column stays NULL for providers that need no secret
// (GoCardless), rather than storing an encryption of nothing.
func (s *SecretBox) Seal(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("bankfeed: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Open reverses Seal. A nil/empty blob opens to the empty string, which is what
// providers without a per-connection secret legitimately have.
func (s *SecretBox) Open(sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	ns := s.aead.NonceSize()
	if len(sealed) < ns {
		return "", errors.New("bankfeed: sealed secret is truncated")
	}
	plain, err := s.aead.Open(nil, sealed[:ns], sealed[ns:], nil)
	if err != nil {
		// Wrong key or tampered ciphertext — the AEAD cannot tell them apart.
		return "", fmt.Errorf("bankfeed: decrypt failed (wrong BANKFEED_ENCRYPTION_KEY?): %w", err)
	}
	return string(plain), nil
}
