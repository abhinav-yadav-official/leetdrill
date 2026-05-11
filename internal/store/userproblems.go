package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"leetdrill/internal/models"
	"leetdrill/internal/srs"
)

// GetUserProblem returns the user_problems row. Returns a zero-valued srs.NewState()
// wrapped in models.UserProblem if missing, so callers always have a starting point.
func GetUserProblem(ctx context.Context, db DBTX, userID, problemID int64) (models.UserProblem, error) {
	const q = `
SELECT user_id, problem_id, ease_factor, interval_days,
       next_due_at, last_attempted_at,
       total_attempts, clean_solves, total_fails, streak, status
FROM user_problems
WHERE user_id = $1 AND problem_id = $2`
	var up models.UserProblem
	var ease float64
	var status string
	err := db.QueryRow(ctx, q, userID, problemID).Scan(
		&up.UserID, &up.ProblemID, &ease, &up.IntervalDays,
		&up.NextDueAt, &up.LastAttemptedAt,
		&up.TotalAttempts, &up.CleanSolves, &up.TotalFails, &up.Streak, &status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Seed default.
		def := srs.NewState()
		return models.UserProblem{
			UserID:     userID,
			ProblemID:  problemID,
			EaseFactor: def.EaseFactor,
			Status:     models.Status(def.Status),
		}, nil
	}
	if err != nil {
		return up, fmt.Errorf("get user_problem (%d,%d): %w", userID, problemID, err)
	}
	up.EaseFactor = ease
	up.Status = models.Status(status)
	return up, nil
}

// UpsertUserProblem writes the row. Used by Apply().
func UpsertUserProblem(ctx context.Context, db DBTX, up models.UserProblem) error {
	const q = `
INSERT INTO user_problems (
    user_id, problem_id, ease_factor, interval_days,
    next_due_at, last_attempted_at,
    total_attempts, clean_solves, total_fails, streak, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (user_id, problem_id) DO UPDATE SET
    ease_factor       = EXCLUDED.ease_factor,
    interval_days     = EXCLUDED.interval_days,
    next_due_at       = EXCLUDED.next_due_at,
    last_attempted_at = EXCLUDED.last_attempted_at,
    total_attempts    = EXCLUDED.total_attempts,
    clean_solves      = EXCLUDED.clean_solves,
    total_fails       = EXCLUDED.total_fails,
    streak            = EXCLUDED.streak,
    status            = EXCLUDED.status`
	_, err := db.Exec(ctx, q,
		up.UserID, up.ProblemID, up.EaseFactor, up.IntervalDays,
		up.NextDueAt, up.LastAttemptedAt,
		up.TotalAttempts, up.CleanSolves, up.TotalFails, up.Streak, string(up.Status),
	)
	if err != nil {
		return fmt.Errorf("upsert user_problem (%d,%d): %w", up.UserID, up.ProblemID, err)
	}
	return nil
}

// InsertSolvedUserProblemIfMissing seeds historical solved problems when
// LeetCode gives us solved slugs but not submission timestamps. Existing SRS
// rows are never overwritten.
func InsertSolvedUserProblemIfMissing(ctx context.Context, db DBTX, up models.UserProblem) (bool, error) {
	const q = `
INSERT INTO user_problems (
    user_id, problem_id, ease_factor, interval_days,
    next_due_at, last_attempted_at,
    total_attempts, clean_solves, total_fails, streak, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (user_id, problem_id) DO NOTHING`
	tag, err := db.Exec(ctx, q,
		up.UserID, up.ProblemID, up.EaseFactor, up.IntervalDays,
		up.NextDueAt, up.LastAttemptedAt,
		up.TotalAttempts, up.CleanSolves, up.TotalFails, up.Streak, string(up.Status),
	)
	if err != nil {
		return false, fmt.Errorf("insert solved user_problem (%d,%d): %w", up.UserID, up.ProblemID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// TriageUserProblem manually adjusts the SRS state.
func TriageUserProblem(ctx context.Context, db DBTX, userID, problemID int64, action string) error {
	switch action {
	case "unleech":
		// Reset fails and put back into rotation as learning.
		_, err := db.Exec(ctx, `
UPDATE user_problems
   SET total_fails = 0, status = 'learning', next_due_at = now()
 WHERE user_id = $1 AND problem_id = $2`, userID, problemID)
		return err
	case "master":
		// Skip all future reviews.
		_, err := db.Exec(ctx, `
UPDATE user_problems
   SET status = 'mastered', next_due_at = NULL
 WHERE user_id = $1 AND problem_id = $2`, userID, problemID)
		return err
	case "reset":
		// Hard reset: delete the SRS row. History (attempts) remains.
		_, err := db.Exec(ctx, `DELETE FROM user_problems WHERE user_id = $1 AND problem_id = $2`, userID, problemID)
		return err
	default:
		return fmt.Errorf("unknown triage action: %s", action)
	}
}

// timeOrNil returns nil for zero-time so we write SQL NULL.
func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
