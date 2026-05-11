package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LeetcodeCookies is the ciphertext pair retrieved from users row.
type LeetcodeCookies struct {
	SessionEnc []byte
	CSRFEnc    []byte
	Username   string
	Valid      bool
}

// SetLeetcodeCookies persists the encrypted cookie pair on a user row,
// marking it valid and bumping cookie_updated_at.
func SetLeetcodeCookies(ctx context.Context, db DBTX, userID int64, sessionEnc, csrfEnc []byte) error {
	const q = `
UPDATE users
   SET leetcode_session_encrypted = $2,
       leetcode_csrf_encrypted    = $3,
       cookie_updated_at          = now(),
       cookie_valid               = TRUE
 WHERE id = $1`
	tag, err := db.Exec(ctx, q, userID, sessionEnc, csrfEnc)
	if err != nil {
		return fmt.Errorf("set leetcode cookies: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set leetcode cookies: user %d not found", userID)
	}
	return nil
}

// MarkCookiesInvalid is called when LeetCode returns 401/403 against authed
// queries.
func MarkCookiesInvalid(ctx context.Context, db DBTX, userID int64) error {
	const q = `UPDATE users SET cookie_valid = FALSE WHERE id = $1`
	_, err := db.Exec(ctx, q, userID)
	if err != nil {
		return fmt.Errorf("mark cookies invalid: %w", err)
	}
	return nil
}

// GetLeetcodeCookies returns the encrypted blobs plus the username. Caller
// decrypts via vault.
func GetLeetcodeCookies(ctx context.Context, db DBTX, userID int64) (LeetcodeCookies, error) {
	const q = `
SELECT leetcode_session_encrypted, leetcode_csrf_encrypted,
       COALESCE(leetcode_username, ''), cookie_valid
FROM users WHERE id = $1`
	var c LeetcodeCookies
	err := db.QueryRow(ctx, q, userID).Scan(&c.SessionEnc, &c.CSRFEnc, &c.Username, &c.Valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, fmt.Errorf("get leetcode cookies: %w", err)
	}
	return c, nil
}
