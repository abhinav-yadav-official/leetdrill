package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AuthKind is the auth session category.
type AuthKind string

const (
	AuthKindWeb AuthKind = "web"
	AuthKindExt AuthKind = "ext"
)

// InsertAuthSession stores the hashed token with a future expiry. Returns the
// row id.
func InsertAuthSession(ctx context.Context, db DBTX, userID int64, kind AuthKind, tokenHash []byte, expiresAt time.Time) (int64, error) {
	const q = `
INSERT INTO auth_sessions (user_id, kind, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id`
	var id int64
	if err := db.QueryRow(ctx, q, userID, string(kind), tokenHash, expiresAt).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert auth_session: %w", err)
	}
	return id, nil
}

// LookupAuthSession returns the user_id behind a (kind, token_hash) pair if it
// hasn't expired. ErrNotFound otherwise.
func LookupAuthSession(ctx context.Context, db DBTX, kind AuthKind, tokenHash []byte) (int64, error) {
	const q = `
SELECT user_id
FROM auth_sessions
WHERE kind = $1 AND token_hash = $2 AND expires_at > now()`
	var userID int64
	err := db.QueryRow(ctx, q, string(kind), tokenHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lookup auth_session: %w", err)
	}
	return userID, nil
}

// DeleteAuthSession removes a token; used on logout.
func DeleteAuthSession(ctx context.Context, db DBTX, kind AuthKind, tokenHash []byte) error {
	const q = `DELETE FROM auth_sessions WHERE kind = $1 AND token_hash = $2`
	_, err := db.Exec(ctx, q, string(kind), tokenHash)
	if err != nil {
		return fmt.Errorf("delete auth_session: %w", err)
	}
	return nil
}

// PurgeExpiredAuthSessions is called periodically.
func PurgeExpiredAuthSessions(ctx context.Context, db DBTX) (int64, error) {
	const q = `DELETE FROM auth_sessions WHERE expires_at <= now()`
	tag, err := db.Exec(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("purge auth_sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
