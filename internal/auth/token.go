package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

// tokenBytes is the length of the raw random secret. 32 bytes → 256 bits.
const tokenBytes = 32

// NewToken returns a fresh URL-safe random token and its SHA-256 hash for
// storage. The raw token is shown to the user/extension once; only the hash
// hits the DB.
func NewToken() (token string, hash []byte, err error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("auth: read random: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	h := sha256.Sum256(buf)
	return token, h[:], nil
}

// HashToken returns the SHA-256 hash for a presented token.
func HashToken(token string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("auth: decode token: %w", err)
	}
	h := sha256.Sum256(raw)
	return h[:], nil
}

// EqualHashes is a constant-time comparison.
func EqualHashes(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
