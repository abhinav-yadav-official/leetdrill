package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Session is the per-day session record.
type Session struct {
	ID                  int64
	UserID              int64
	Date                time.Time
	ProblemIDs          []int64
	CompletedProblemIDs []int64
	StartedAt           time.Time
	CompletedAt         *time.Time
}

// GetTodaySession returns today's session, if it exists.
func GetTodaySession(ctx context.Context, db DBTX, userID int64) (*Session, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	return GetSessionByDate(ctx, db, userID, today)
}

// GetSessionByDate returns a user's session for date.
func GetSessionByDate(ctx context.Context, db DBTX, userID int64, date time.Time) (*Session, error) {
	const q = `
SELECT id, user_id, date, problem_ids, completed_problem_ids, started_at, completed_at
FROM sessions
WHERE user_id = $1 AND date = $2::date`
	var s Session
	var pids, cids []byte
	err := db.QueryRow(ctx, q, userID, date).Scan(
		&s.ID, &s.UserID, &s.Date, &pids, &cids, &s.StartedAt, &s.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session by date: %w", err)
	}
	_ = json.Unmarshal(pids, &s.ProblemIDs)
	_ = json.Unmarshal(cids, &s.CompletedProblemIDs)
	return &s, nil
}

// GetSession returns one session by id, restricted to the calling user.
func GetSession(ctx context.Context, db DBTX, userID, sessionID int64) (*Session, error) {
	const q = `
SELECT id, user_id, date, problem_ids, completed_problem_ids, started_at, completed_at
FROM sessions
WHERE user_id = $1 AND id = $2`
	var s Session
	var pids, cids []byte
	err := db.QueryRow(ctx, q, userID, sessionID).Scan(
		&s.ID, &s.UserID, &s.Date, &pids, &cids, &s.StartedAt, &s.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	_ = json.Unmarshal(pids, &s.ProblemIDs)
	_ = json.Unmarshal(cids, &s.CompletedProblemIDs)
	return &s, nil
}

// EnsureTodaySession returns the existing today's row or builds a new one
// from due reviews + fresh problems. Caller passes target session size.
func EnsureTodaySession(ctx context.Context, db DBTX, userID int64, size int) (*Session, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)

	// Try existing.
	const lookup = `
SELECT id, user_id, date, problem_ids, completed_problem_ids, started_at, completed_at
FROM sessions
WHERE user_id = $1 AND date = $2::date`
	s := Session{}
	var pids, cids []byte
	foundSession := false
	err := db.QueryRow(ctx, lookup, userID, today).Scan(
		&s.ID, &s.UserID, &s.Date, &pids, &cids, &s.StartedAt, &s.CompletedAt,
	)
	if err == nil {
		foundSession = true
		_ = json.Unmarshal(pids, &s.ProblemIDs)
		_ = json.Unmarshal(cids, &s.CompletedProblemIDs)
		if !sessionNeedsProblemBuild(s) {
			return &s, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	if size <= 0 || size > 25 {
		size = 5
	}

	// Pick problems: up to ceil(size*0.6) due reviews, rest new.
	reviews := size * 3 / 5
	if reviews < 1 {
		reviews = 1
	}
	due, err := pickDueProblemIDs(ctx, db, userID, reviews)
	if err != nil {
		return nil, err
	}
	need := size - len(due)
	news, err := pickNewProblemIDs(ctx, db, userID, need)
	if err != nil {
		return nil, err
	}
	pool := append(append([]int64{}, due...), news...)
	pidsJSON, _ := json.Marshal(pool)

	if foundSession {
		const update = `
UPDATE sessions
   SET problem_ids = $2::jsonb,
       completed_problem_ids = '[]'::jsonb,
       completed_at = NULL
 WHERE id = $1
RETURNING started_at`
		if err := db.QueryRow(ctx, update, s.ID, pidsJSON).Scan(&s.StartedAt); err != nil {
			return nil, fmt.Errorf("update empty session: %w", err)
		}
		s.ProblemIDs = pool
		s.CompletedProblemIDs = nil
		s.CompletedAt = nil
		return &s, nil
	}

	const insert = `
INSERT INTO sessions (user_id, date, problem_ids, completed_problem_ids)
VALUES ($1, $2::date, $3::jsonb, '[]'::jsonb)
RETURNING id, started_at`
	var id int64
	var started time.Time
	if err := db.QueryRow(ctx, insert, userID, today, pidsJSON).Scan(&id, &started); err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return &Session{
		ID:         id,
		UserID:     userID,
		Date:       today,
		ProblemIDs: pool,
		StartedAt:  started,
	}, nil
}

func sessionNeedsProblemBuild(s Session) bool {
	return len(s.ProblemIDs) == 0 && s.CompletedAt == nil
}

func pickDueProblemIDs(ctx context.Context, db DBTX, userID int64, n int) ([]int64, error) {
	const q = `
SELECT up.problem_id
FROM user_problems up
JOIN users u ON u.id = up.user_id
WHERE up.user_id = $1
  AND (u.vacation_until IS NULL OR u.vacation_until <= now())
  AND up.status NOT IN ('leech','new','mastered')
  AND up.next_due_at IS NOT NULL
  AND up.next_due_at <= now() + interval '1 day'
ORDER BY up.next_due_at ASC
LIMIT $2`
	rows, err := db.Query(ctx, q, userID, n)
	if err != nil {
		return nil, fmt.Errorf("pick due: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func pickNewProblemIDs(ctx context.Context, db DBTX, userID int64, n int) ([]int64, error) {
	if n <= 0 {
		return nil, nil
	}
	const q = `
SELECT p.id
FROM problems p
LEFT JOIN user_problems up
  ON up.user_id = $1 AND up.problem_id = p.id
WHERE up.user_id IS NULL
  AND p.paid_only = FALSE
  AND p.difficulty = 'Medium'
ORDER BY p.ac_rate DESC NULLS LAST
LIMIT $2`
	rows, err := db.Query(ctx, q, userID, n)
	if err != nil {
		return nil, fmt.Errorf("pick new: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// MarkProblemCompleted appends a problem_id to completed_problem_ids if it's
// in problem_ids and not already completed. Returns true if newly completed.
// Used by the htmx poll endpoint to detect verdict arrival.
func MarkProblemCompleted(ctx context.Context, db DBTX, sessionID, problemID int64) (bool, error) {
	const q = `
UPDATE sessions
   SET completed_problem_ids = completed_problem_ids || to_jsonb($2::bigint),
       completed_at = CASE
         WHEN jsonb_array_length(completed_problem_ids || to_jsonb($2::bigint))
              >= jsonb_array_length(problem_ids)
           THEN now()
         ELSE completed_at
       END
 WHERE id = $1
   AND problem_ids @> to_jsonb($2::bigint)
   AND NOT (completed_problem_ids @> to_jsonb($2::bigint))`
	tag, err := db.Exec(ctx, q, sessionID, problemID)
	if err != nil {
		return false, fmt.Errorf("mark completed: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// SessionCurrent returns the current problem id in a session (first in
// problem_ids not in completed_problem_ids), or 0 if the session is done.
func SessionCurrent(ctx context.Context, db DBTX, sessionID int64) (int64, error) {
	const q = `
SELECT s.problem_ids - s.completed_problem_ids AS remaining
FROM sessions s WHERE s.id = $1`
	// jsonb minus needs explicit operator; do it client-side instead.
	const q2 = `SELECT problem_ids, completed_problem_ids FROM sessions WHERE id = $1`
	var pids, cids []byte
	if err := db.QueryRow(ctx, q2, sessionID).Scan(&pids, &cids); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("session current: %w", err)
	}
	var p, c []int64
	_ = json.Unmarshal(pids, &p)
	_ = json.Unmarshal(cids, &c)
	done := map[int64]bool{}
	for _, id := range c {
		done[id] = true
	}
	for _, id := range p {
		if !done[id] {
			return id, nil
		}
	}
	_ = q // silence unused
	return 0, nil
}

// LatestAttemptSince returns the most recent attempt for (user, problem) with
// completed_at >= since. Used by htmx poll to detect verdict arrival.
type LatestAttempt struct {
	ID            int64
	CompletedAt   time.Time
	Verdict       string
	DerivedRating string
	Found         bool
}

func LatestAttemptSince(ctx context.Context, db DBTX, userID, problemID int64, since time.Time) (LatestAttempt, error) {
	const q = `
SELECT id, completed_at, verdict, derived_rating
FROM attempts
WHERE user_id = $1 AND problem_id = $2 AND completed_at >= $3
ORDER BY completed_at DESC
LIMIT 1`
	var a LatestAttempt
	err := db.QueryRow(ctx, q, userID, problemID, since).Scan(&a.ID, &a.CompletedAt, &a.Verdict, &a.DerivedRating)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return a, fmt.Errorf("latest attempt: %w", err)
	}
	a.Found = true
	return a, nil
}
