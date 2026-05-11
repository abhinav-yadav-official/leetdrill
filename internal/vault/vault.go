// Package vault encrypts and decrypts small secrets at rest — specifically the
// user's LeetCode session cookies. AES-256-GCM with a key sourced from env.
//
// Storage format: 1-byte version || 12-byte nonce || ciphertext (incl. tag).
// Versioning lets us rotate ciphers later without losing the ability to
// decrypt existing rows.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	versionV1   byte = 1
	keyBytes         = 32 // AES-256
	nonceBytes       = 12 // GCM standard
)

// Vault is a configured AEAD with a 32-byte key.
type Vault struct {
	aead cipher.AEAD
}

// New constructs a Vault from a 32-byte key.
func New(key []byte) (*Vault, error) {
	if len(key) != keyBytes {
		return nil, fmt.Errorf("vault: key must be %d bytes, got %d", keyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("vault: aes init: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: gcm init: %w", err)
	}
	return &Vault{aead: aead}, nil
}

// FromEnv reads LEETDRILL_COOKIE_KEY (base64-encoded 32 bytes) and constructs
// a Vault.
func FromEnv() (*Vault, error) {
	raw := os.Getenv("LEETDRILL_COOKIE_KEY")
	if raw == "" {
		return nil, errors.New("vault: LEETDRILL_COOKIE_KEY not set")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		// tolerate URL-safe encoding too
		key, err = base64.URLEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("vault: decode key: %w", err)
		}
	}
	return New(key)
}

// Seal encrypts plaintext and returns version || nonce || ciphertext.
func (v *Vault) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, nonceBytes)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("vault: read nonce: %w", err)
	}
	ct := v.aead.Seal(nil, nonce, plain, nil)
	out := make([]byte, 0, 1+nonceBytes+len(ct))
	out = append(out, versionV1)
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// Open reverses Seal.
func (v *Vault) Open(blob []byte) ([]byte, error) {
	if len(blob) < 1+nonceBytes+v.aead.Overhead() {
		return nil, errors.New("vault: ciphertext too short")
	}
	if blob[0] != versionV1 {
		return nil, fmt.Errorf("vault: unknown version %d", blob[0])
	}
	nonce := blob[1 : 1+nonceBytes]
	ct := blob[1+nonceBytes:]
	pt, err := v.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: open: %w", err)
	}
	return pt, nil
}
