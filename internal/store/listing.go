package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"leetdrill/internal/models"
)

// ProblemListItem is the row shape used by the /problems table view.
type ProblemListItem struct {
	ProblemID     int64
	Slug          string
	LeetcodeID    string
	Title         string
	Difficulty    models.Difficulty
	URL           string
	Topics        []models.Tag
	Status        models.Status // 'new' if no user_problems row
	NextDueAt     *time.Time
	IntervalDays  int
	Streak        int
	TotalAttempts int
	TotalFails    int
}

// ProblemFilters contains the optional filters used by the /problems listing.
type ProblemFilters struct {
	State      string
	Pattern    string
	Difficulty string
	Acceptance string
}

// ListProblemsForUser returns problems joined with the user's SRS state.
// Filter values: "" (all), "due", "learning", "review", "mastered", "leech", "new".
func ListProblemsForUser(ctx context.Context, db DBTX, userID int64, filters ProblemFilters, limit, offset int) ([]ProblemListItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	parts := buildProblemQuery(userID, filters)
	args := append([]any{}, parts.args...)
	args = append(args, limit, offset)
	limitParam := len(args) - 1
	offsetParam := len(args)

	q := `
SELECT p.id, p.leetcode_slug, COALESCE(p.leetcode_frontend_id, ''), p.title, p.difficulty, p.url, p.topic_tags,
       COALESCE(up.status, 'new') AS status,
       up.next_due_at, COALESCE(up.interval_days, 0),
       COALESCE(up.streak, 0), COALESCE(up.total_attempts, 0),
       COALESCE(up.total_fails, 0)
FROM problems p
LEFT JOIN user_problems up
  ON up.user_id = $1 AND up.problem_id = p.id
` + parts.joins + parts.where + fmt.Sprintf(`
ORDER BY p.leetcode_frontend_id::int ASC NULLS LAST, p.id ASC
LIMIT $%d OFFSET $%d`, limitParam, offsetParam)

	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list problems: %w", err)
	}
	defer rows.Close()
	var out []ProblemListItem
	for rows.Next() {
		var r ProblemListItem
		var tags []byte
		var diff, status string
		if err := rows.Scan(
			&r.ProblemID, &r.Slug, &r.LeetcodeID, &r.Title, &diff, &r.URL, &tags,
			&status, &r.NextDueAt, &r.IntervalDays, &r.Streak, &r.TotalAttempts, &r.TotalFails,
		); err != nil {
			return nil, fmt.Errorf("scan problem: %w", err)
		}
		r.Difficulty = models.Difficulty(diff)
		r.Status = models.Status(status)
		if len(tags) > 0 {
			_ = json.Unmarshal(tags, &r.Topics)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func CountProblemsForUser(ctx context.Context, db DBTX, userID int64, filters ProblemFilters) (int, error) {
	parts := buildProblemQuery(userID, filters)
	q := `
SELECT COUNT(*)
FROM problems p
LEFT JOIN user_problems up
  ON up.user_id = $1 AND up.problem_id = p.id
` + parts.joins + parts.where
	var count int
	if err := db.QueryRow(ctx, q, parts.args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count problems: %w", err)
	}
	return count, nil
}

type problemQueryParts struct {
	joins string
	where string
	args  []any
}

func buildProblemQuery(userID int64, filters ProblemFilters) problemQueryParts {
	args := []any{userID}
	var joins []string
	var where []string

	if pattern := strings.TrimSpace(filters.Pattern); pattern != "" {
		args = append(args, pattern)
		joins = append(joins, fmt.Sprintf(`
JOIN problem_patterns pp ON pp.problem_id = p.id
JOIN patterns pat ON pat.id = pp.pattern_id AND pat.slug = $%d`, len(args)))
	}

	if condition := problemStateCondition(filters.State); condition != "" {
		where = append(where, condition)
	}
	switch filters.Difficulty {
	case string(models.DifficultyEasy), string(models.DifficultyMedium), string(models.DifficultyHard):
		args = append(args, filters.Difficulty)
		where = append(where, fmt.Sprintf("p.difficulty = $%d", len(args)))
	}
	if lower, ok := acceptanceLowerBound(filters.Acceptance); ok {
		args = append(args, lower)
		lowerParam := len(args)
		if lower >= 90 {
			where = append(where, fmt.Sprintf("p.ac_rate >= $%d", lowerParam))
		} else {
			args = append(args, lower+10)
			where = append(where, fmt.Sprintf("p.ac_rate >= $%d AND p.ac_rate < $%d", lowerParam, len(args)))
		}
	}

	parts := problemQueryParts{args: args}
	if len(joins) > 0 {
		parts.joins = strings.Join(joins, "\n") + "\n"
	}
	if len(where) > 0 {
		parts.where = "WHERE " + strings.Join(where, "\n  AND ") + "\n"
	}
	return parts
}

func problemStateCondition(state string) string {
	switch state {
	case "due":
		return `up.next_due_at IS NOT NULL AND up.next_due_at <= now()
		         AND up.status NOT IN ('leech','new','mastered')`
	case "learning", "review", "mastered", "leech":
		return fmt.Sprintf(`up.status = '%s'`, state)
	case "new":
		return `up.user_id IS NULL`
	default:
		return ""
	}
}

func acceptanceLowerBound(raw string) (float64, bool) {
	bound, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || bound < 0 || bound > 90 || bound%10 != 0 {
		return 0, false
	}
	return float64(bound), true
}

// ProblemDetail bundles a problem with the user's SRS state and history.
type ProblemDetail struct {
	Problem  models.Problem
	State    models.UserProblem
	Attempts []models.Attempt
}

// GetProblemDetail returns a problem + the calling user's SRS state +
// up to N recent attempts.
func GetProblemDetail(ctx context.Context, db DBTX, userID int64, slug string, attemptLimit int) (*ProblemDetail, error) {
	p, err := GetProblemBySlug(ctx, db, slug)
	if err != nil {
		return nil, err
	}
	up, err := GetUserProblem(ctx, db, userID, p.ID)
	if err != nil {
		return nil, err
	}
	if attemptLimit <= 0 || attemptLimit > 100 {
		attemptLimit = 25
	}
	const q = `
SELECT id, started_at, completed_at, verdict, submission_count_in_session,
       time_taken_sec, runtime_ms, memory_kb,
       COALESCE(language,''), COALESCE(code,''),
       derived_rating, COALESCE(journal,''), mistake_tags,
       COALESCE(leetcode_submission_id,'')
FROM attempts
WHERE user_id = $1 AND problem_id = $2
ORDER BY completed_at DESC
LIMIT $3`
	rows, err := db.Query(ctx, q, userID, p.ID, attemptLimit)
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	defer rows.Close()
	var attempts []models.Attempt
	for rows.Next() {
		var a models.Attempt
		var tags []byte
		if err := rows.Scan(
			&a.ID, &a.StartedAt, &a.CompletedAt, &a.Verdict, &a.SubmissionCountInSession,
			&a.TimeTakenSec, &a.RuntimeMs, &a.MemoryKB,
			&a.Language, &a.Code,
			&a.DerivedRating, &a.Journal, &tags, &a.LeetcodeSubmissionID,
		); err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}
		if len(tags) > 0 {
			_ = json.Unmarshal(tags, &a.MistakeTags)
		}
		attempts = append(attempts, a)
	}
	a := &ProblemDetail{Problem: *p, State: up, Attempts: attempts}
	return a, nil
}

// UpdateLatestAttemptJournal sets the journal text on the most-recent attempt
// for (user, problem). Returns ErrNotFound if no attempt exists.
func UpdateLatestAttemptJournal(ctx context.Context, db DBTX, userID, problemID int64, journal string) error {
	const q = `
UPDATE attempts
   SET journal = $3
 WHERE id = (
   SELECT id FROM attempts
   WHERE user_id = $1 AND problem_id = $2
   ORDER BY completed_at DESC
   LIMIT 1
 )`
	tag, err := db.Exec(ctx, q, userID, problemID, journal)
	if err != nil {
		return fmt.Errorf("update journal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PatternStrength is the aggregate per-pattern signal shown in the patterns view.
type PatternStrength struct {
	PatternID     int64
	Slug          string
	Name          string
	TotalProblems int // problems carrying this pattern
	UserAttempts  int // attempts on problems carrying this pattern
	CleanSolves   int // distinct problems solved on LeetCode
	Failures      int // derived_rating = 'failed'
	StrengthPct   int // 0-100, computed in SQL
}

func ListPatternsWithStrength(ctx context.Context, db DBTX, userID int64) ([]PatternStrength, error) {
	q := patternsWithStrengthSQL()
	rows, err := db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list patterns: %w", err)
	}
	defer rows.Close()
	var out []PatternStrength
	for rows.Next() {
		var p PatternStrength
		var strength *int
		if err := rows.Scan(
			&p.PatternID, &p.Slug, &p.Name,
			&p.TotalProblems, &p.UserAttempts, &p.CleanSolves, &p.Failures, &strength,
		); err != nil {
			return nil, fmt.Errorf("scan pattern: %w", err)
		}
		if strength != nil {
			p.StrengthPct = *strength
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func patternsWithStrengthSQL() string {
	return `
SELECT
  pat.id, pat.slug, pat.name,
  COUNT(DISTINCT pp.problem_id) AS total_problems,
  COALESCE(COUNT(DISTINCT CASE WHEN a.id IS NOT NULL THEN a.problem_id END), 0) AS attempts,
  COALESCE(COUNT(DISTINCT CASE WHEN up.clean_solves > 0 OR a.verdict = 'AC' THEN pp.problem_id END), 0) AS clean,
  COALESCE(COUNT(DISTINCT CASE WHEN up.total_fails > 0 THEN up.problem_id END), 0) AS failed,
  CASE WHEN COUNT(DISTINCT pp.problem_id) = 0 THEN 0
       ELSE (COUNT(DISTINCT CASE WHEN up.clean_solves > 0 OR a.verdict = 'AC' THEN pp.problem_id END)::int * 100)
            / NULLIF(COUNT(DISTINCT pp.problem_id), 0)::int
  END AS strength
FROM patterns pat
LEFT JOIN problem_patterns pp ON pp.pattern_id = pat.id
LEFT JOIN user_problems up
       ON up.problem_id = pp.problem_id AND up.user_id = $1
LEFT JOIN attempts a
       ON a.problem_id = pp.problem_id AND a.user_id = $1
GROUP BY pat.id, pat.slug, pat.name
HAVING COUNT(DISTINCT pp.problem_id) > 0
ORDER BY (strength) ASC NULLS LAST, total_problems DESC, pat.name ASC`
}

// ListPatternProblems returns problems carrying a given pattern slug for the user view.
func ListPatternProblems(ctx context.Context, db DBTX, userID int64, patternSlug string, limit int) ([]ProblemListItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
SELECT p.id, p.leetcode_slug, COALESCE(p.leetcode_frontend_id, ''), p.title, p.difficulty, p.url, p.topic_tags,
       COALESCE(up.status, 'new') AS status,
       up.next_due_at, COALESCE(up.interval_days, 0),
       COALESCE(up.streak, 0), COALESCE(up.total_attempts, 0),
       COALESCE(up.total_fails, 0)
FROM problems p
JOIN problem_patterns pp ON pp.problem_id = p.id
JOIN patterns pat ON pat.id = pp.pattern_id AND pat.slug = $2
LEFT JOIN user_problems up
  ON up.user_id = $1 AND up.problem_id = p.id
ORDER BY p.leetcode_frontend_id::int ASC NULLS LAST, p.id ASC
LIMIT $3`
	rows, err := db.Query(ctx, q, userID, patternSlug, limit)
	if err != nil {
		return nil, fmt.Errorf("list pattern problems: %w", err)
	}
	defer rows.Close()
	var out []ProblemListItem
	for rows.Next() {
		var r ProblemListItem
		var tags []byte
		var diff, status string
		if err := rows.Scan(
			&r.ProblemID, &r.Slug, &r.LeetcodeID, &r.Title, &diff, &r.URL, &tags,
			&status, &r.NextDueAt, &r.IntervalDays, &r.Streak, &r.TotalAttempts, &r.TotalFails,
		); err != nil {
			return nil, fmt.Errorf("scan problem: %w", err)
		}
		r.Difficulty = models.Difficulty(diff)
		r.Status = models.Status(status)
		if len(tags) > 0 {
			_ = json.Unmarshal(tags, &r.Topics)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LookupProblemID returns the id for a slug.
func LookupProblemID(ctx context.Context, db DBTX, slug string) (int64, error) {
	var id int64
	err := db.QueryRow(ctx, `SELECT id FROM problems WHERE leetcode_slug = $1`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
