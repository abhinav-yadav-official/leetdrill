// Package auth holds password hashing, token issuance/verification, session
// middleware, and the context plumbing for the current user id.
package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is intentionally a couple notches above the default; bumping
// once a year is fine — login frequency is low.
const bcryptCost = 12

// HashPassword returns a bcrypt hash.
func HashPassword(pw string) (string, error) {
	if pw == "" {
		return "", errors.New("auth: empty password")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// VerifyPassword returns nil on match. ErrMismatchedHashAndPassword from bcrypt
// is surfaced as ErrBadPassword.
func VerifyPassword(hash, pw string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrBadPassword
	}
	return err
}

// ErrBadPassword is returned by VerifyPassword when the candidate doesn't match.
var ErrBadPassword = errors.New("auth: bad password")
