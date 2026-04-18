package handlers_test

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Verifies seed migration password hash matches documented demo password.
func TestSeedDemoPasswordHash(t *testing.T) {
	const hash = "$2a$10$pAimBhbqKiEBvTXqKhhWlOfbgNNFoa5o3GlGLR9EGxKh5hedcwUVK"
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("admin123")); err != nil {
		t.Fatalf("seed hash does not match admin123: %v", err)
	}
}
