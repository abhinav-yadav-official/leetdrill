package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"leetdrill/internal/models"
)

// NextProblem is the small record returned to the extension popup.
type NextProblem struct {
	Slug       string
	URL        string
	Title      string
	Difficulty models.Difficulty
	Reason     string // "review" | "new"
}

// SelectNextDue returns the next problem to surface for a user:
//   - earliest user_problems.next_due_at among non-leech, non-new rows that
//     are due now-ish (within 24h of next_due_at OR overdue)
//   - falls back to a fresh problem the user hasn't started, picked by lowest
//     ac_rate among Medium difficulty for some signal that it's a learning
//     opportunity rather than the easiest unsolved
//
// Returns ErrNotFound if absolutely nothing is selectable.
func SelectNextDue(ctx context.Context, db DBTX, userID int64) (NextProblem, error) {
	const dueQ = `
SELECT p.leetcode_slug, p.url, p.title, p.difficulty
FROM user_problems up
JOIN problems p ON p.id = up.problem_id
WHERE up.user_id = $1
  AND up.status NOT IN ('leech','new','mastered')
  AND up.next_due_at IS NOT NULL
  AND up.next_due_at <= now() + interval '1 day'
ORDER BY up.next_due_at ASC
LIMIT 1`
	var np NextProblem
	var diff string
	err := db.QueryRow(ctx, dueQ, userID).Scan(&np.Slug, &np.URL, &np.Title, &diff)
	if err == nil {
		np.Difficulty = models.Difficulty(diff)
		np.Reason = "review"
		return np, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return np, fmt.Errorf("select due: %w", err)
	}

	const newQ = `
SELECT p.leetcode_slug, p.url, p.title, p.difficulty
FROM problems p
LEFT JOIN user_problems up
       ON up.user_id = $1 AND up.problem_id = p.id
WHERE up.user_id IS NULL
  AND p.paid_only = FALSE
  AND p.difficulty = 'Medium'
ORDER BY p.ac_rate ASC NULLS LAST
LIMIT 1`
	err = db.QueryRow(ctx, newQ, userID).Scan(&np.Slug, &np.URL, &np.Title, &diff)
	if errors.Is(err, pgx.ErrNoRows) {
		return np, ErrNotFound
	}
	if err != nil {
		return np, fmt.Errorf("select new: %w", err)
	}
	np.Difficulty = models.Difficulty(diff)
	np.Reason = "new"
	return np, nil
}
