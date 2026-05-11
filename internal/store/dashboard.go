package store

import (
	"context"
	"fmt"
	"time"

	"leetdrill/internal/models"
)

// DashboardCounts is what the home page header needs in one round-trip.
type DashboardCounts struct {
	DueNow       int
	DueIn24h     int
	Learning     int
	Review       int
	Mastered     int
	Leech        int
	TotalProblems int
}

func CountsForDashboard(ctx context.Context, db DBTX, userID int64) (DashboardCounts, error) {
	const q = `
SELECT
  COUNT(*) FILTER (WHERE up.next_due_at IS NOT NULL
                     AND up.next_due_at <= now()
                     AND up.status NOT IN ('leech','new','mastered')
                     AND (u.vacation_until IS NULL OR u.vacation_until <= now())) AS due_now,
  COUNT(*) FILTER (WHERE up.next_due_at IS NOT NULL
                     AND up.next_due_at <= now() + interval '1 day'
                     AND up.status NOT IN ('leech','new','mastered')
                     AND (u.vacation_until IS NULL OR u.vacation_until <= now())) AS due_24h,
  COUNT(*) FILTER (WHERE up.status = 'learning') AS learning,
  COUNT(*) FILTER (WHERE up.status = 'review')   AS review,
  COUNT(*) FILTER (WHERE up.status = 'mastered') AS mastered,
  COUNT(*) FILTER (WHERE up.status = 'leech')    AS leech,
  (SELECT COUNT(*) FROM problems)               AS total_problems
FROM user_problems up
JOIN users u ON u.id = up.user_id
WHERE up.user_id = $1`
	var c DashboardCounts
	err := db.QueryRow(ctx, q, userID).Scan(
		&c.DueNow, &c.DueIn24h, &c.Learning, &c.Review, &c.Mastered, &c.Leech, &c.TotalProblems,
	)
	if err != nil {
		return c, fmt.Errorf("dashboard counts: %w", err)
	}
	return c, nil
}

// RecentAttempt is a denormalized row for the dashboard's "last 10 attempts".
type RecentAttempt struct {
	CompletedAt   time.Time
	Verdict       string
	DerivedRating string
	Slug          string
	Title         string
	Difficulty    models.Difficulty
}

func RecentAttempts(ctx context.Context, db DBTX, userID int64, limit int) ([]RecentAttempt, error) {
	if limit <= 0 || limit > 200 {
		limit = 10
	}
	const q = `
SELECT a.completed_at, a.verdict, a.derived_rating,
       p.leetcode_slug, p.title, p.difficulty
FROM attempts a
JOIN problems p ON p.id = a.problem_id
WHERE a.user_id = $1
ORDER BY a.completed_at DESC
LIMIT $2`
	rows, err := db.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent attempts: %w", err)
	}
	defer rows.Close()
	var out []RecentAttempt
	for rows.Next() {
		var r RecentAttempt
		var diff string
		if err := rows.Scan(&r.CompletedAt, &r.Verdict, &r.DerivedRating, &r.Slug, &r.Title, &diff); err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}
		r.Difficulty = models.Difficulty(diff)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CurrentStreakDays returns the number of consecutive days (ending today)
// on which the user logged at least one attempt. Skipping a day breaks it.
func CurrentStreakDays(ctx context.Context, db DBTX, userID int64) (int, error) {
	const q = `
WITH days AS (
  SELECT DISTINCT date_trunc('day', completed_at AT TIME ZONE 'UTC') AS d
  FROM attempts
  WHERE user_id = $1
)
SELECT COUNT(*)
FROM (
  SELECT d, row_number() OVER (ORDER BY d DESC) AS rn
  FROM days
) t
WHERE t.d = (current_date AT TIME ZONE 'UTC') - ((t.rn - 1) * interval '1 day')`
	var n int
	if err := db.QueryRow(ctx, q, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("streak: %w", err)
	}
	return n, nil
}
