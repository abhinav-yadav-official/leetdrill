package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnsureSingleUser returns the id of the single-user row, creating it if
// missing. Used when SINGLE_USER=true. Email is optional.
func EnsureSingleUser(ctx context.Context, db DBTX, email, leetcodeUsername string) (int64, error) {
	const lookup = `SELECT id FROM users ORDER BY id LIMIT 1`
	var id int64
	err := db.QueryRow(ctx, lookup).Scan(&id)
	if err == nil {
		// Refresh leetcode username if provided.
		if leetcodeUsername != "" {
			if _, err := db.Exec(ctx,
				`UPDATE users SET leetcode_username = $2 WHERE id = $1 AND COALESCE(leetcode_username,'') <> $2`,
				id, leetcodeUsername,
			); err != nil {
				return 0, fmt.Errorf("update leetcode username: %w", err)
			}
		}
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("lookup user: %w", err)
	}

	const insert = `
INSERT INTO users (email, leetcode_username)
VALUES (NULLIF($1,''), NULLIF($2,''))
RETURNING id`
	if err := db.QueryRow(ctx, insert, email, leetcodeUsername).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert user: %w", err)
	}
	return id, nil
}

type SyncUser struct {
	ID               int64
	LeetcodeUsername string
}

func ListUsersForRecentSync(ctx context.Context, db DBTX) ([]SyncUser, error) {
	const q = `
SELECT id, leetcode_username
FROM users
WHERE COALESCE(leetcode_username, '') <> ''
ORDER BY id`
	rows, err := db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list users for recent sync: %w", err)
	}
	defer rows.Close()
	var out []SyncUser
	for rows.Next() {
		var u SyncUser
		if err := rows.Scan(&u.ID, &u.LeetcodeUsername); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func GetVacationUntil(ctx context.Context, db DBTX, userID int64) (*time.Time, error) {
	var until *time.Time
	err := db.QueryRow(ctx, `SELECT vacation_until FROM users WHERE id = $1`, userID).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return until, err
}

func SetVacationUntil(ctx context.Context, db DBTX, userID int64, until *time.Time) error {
	_, err := db.Exec(ctx, `UPDATE users SET vacation_until = $2 WHERE id = $1`, userID, until)
	return err
}
