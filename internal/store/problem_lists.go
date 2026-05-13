package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"leetdrill/internal/models"
)

type CuratedList struct {
	ID          int64
	Slug        string
	Name        string
	Description string
	SourceURL   string
	TotalItems  int
	SolvedItems int
}

type CuratedListProblem struct {
	Position int
	Section  string
	ProblemListItem
}

func ListCuratedLists(ctx context.Context, db DBTX, userID int64) ([]CuratedList, error) {
	q := `
SELECT pl.id, pl.slug, pl.name, COALESCE(pl.description, ''), COALESCE(pl.source_url, ''),
       COUNT(pli.problem_id) AS total_items,
       COUNT(pli.problem_id) FILTER (WHERE ` + solvedProblemCondition() + `) AS solved_items
FROM problem_lists pl
LEFT JOIN problem_list_items pli ON pli.list_id = pl.id
LEFT JOIN problems p ON p.id = pli.problem_id
LEFT JOIN user_problems up ON up.user_id = $1 AND up.problem_id = p.id
GROUP BY pl.id
ORDER BY pl.sort_order ASC, pl.name ASC`
	rows, err := db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list curated lists: %w", err)
	}
	defer rows.Close()

	var out []CuratedList
	for rows.Next() {
		var item CuratedList
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Description, &item.SourceURL, &item.TotalItems, &item.SolvedItems); err != nil {
			return nil, fmt.Errorf("scan curated list: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func GetCuratedList(ctx context.Context, db DBTX, userID int64, slug string) (*CuratedList, error) {
	q := `
SELECT pl.id, pl.slug, pl.name, COALESCE(pl.description, ''), COALESCE(pl.source_url, ''),
       COUNT(pli.problem_id) AS total_items,
       COUNT(pli.problem_id) FILTER (WHERE ` + solvedProblemCondition() + `) AS solved_items
FROM problem_lists pl
LEFT JOIN problem_list_items pli ON pli.list_id = pl.id
LEFT JOIN problems p ON p.id = pli.problem_id
LEFT JOIN user_problems up ON up.user_id = $1 AND up.problem_id = p.id
WHERE pl.slug = $2
GROUP BY pl.id`
	var item CuratedList
	err := db.QueryRow(ctx, q, userID, slug).Scan(&item.ID, &item.Slug, &item.Name, &item.Description, &item.SourceURL, &item.TotalItems, &item.SolvedItems)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get curated list: %w", err)
	}
	return &item, nil
}

func ListCuratedListProblems(ctx context.Context, db DBTX, userID int64, slug string) ([]CuratedListProblem, error) {
	const q = `
SELECT pli.position, COALESCE(pli.section, ''),
       p.id, p.leetcode_slug, COALESCE(p.leetcode_frontend_id, ''), p.title, p.difficulty, p.url, p.topic_tags,
       COALESCE(up.status, 'new') AS status,
       up.next_due_at, COALESCE(up.interval_days, 0),
       COALESCE(up.streak, 0), COALESCE(up.total_attempts, 0),
       COALESCE(up.total_fails, 0)
FROM problem_list_items pli
JOIN problem_lists pl ON pl.id = pli.list_id
JOIN problems p ON p.id = pli.problem_id
LEFT JOIN user_problems up
  ON up.user_id = $1 AND up.problem_id = p.id
WHERE pl.slug = $2
ORDER BY pli.position ASC`
	rows, err := db.Query(ctx, q, userID, slug)
	if err != nil {
		return nil, fmt.Errorf("list curated list problems: %w", err)
	}
	defer rows.Close()

	var out []CuratedListProblem
	for rows.Next() {
		var item CuratedListProblem
		var tags []byte
		var diff, status string
		var nextDue *time.Time
		if err := rows.Scan(
			&item.Position, &item.Section,
			&item.ProblemID, &item.Slug, &item.LeetcodeID, &item.Title, &diff, &item.URL, &tags,
			&status, &nextDue, &item.IntervalDays, &item.Streak, &item.TotalAttempts, &item.TotalFails,
		); err != nil {
			return nil, fmt.Errorf("scan curated list problem: %w", err)
		}
		item.Difficulty = models.Difficulty(diff)
		item.Status = models.Status(status)
		item.NextDueAt = nextDue
		if len(tags) > 0 {
			_ = json.Unmarshal(tags, &item.Topics)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func ListAllLeetcodeProblems(ctx context.Context, db DBTX, userID int64) ([]CuratedListProblem, error) {
	const q = `
SELECT ROW_NUMBER() OVER (ORDER BY p.leetcode_frontend_id::int ASC NULLS LAST, p.id ASC)::int AS position,
       p.id, p.leetcode_slug, COALESCE(p.leetcode_frontend_id, ''), p.title, p.difficulty, p.url, p.topic_tags,
       COALESCE(up.status, 'new') AS status,
       up.next_due_at, COALESCE(up.interval_days, 0),
       COALESCE(up.streak, 0), COALESCE(up.total_attempts, 0),
       COALESCE(up.total_fails, 0)
FROM problems p
LEFT JOIN user_problems up
  ON up.user_id = $1 AND up.problem_id = p.id
ORDER BY p.leetcode_frontend_id::int ASC NULLS LAST, p.id ASC`
	rows, err := db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list all leetcode problems: %w", err)
	}
	defer rows.Close()

	var out []CuratedListProblem
	for rows.Next() {
		var item CuratedListProblem
		var tags []byte
		var diff, status string
		if err := rows.Scan(
			&item.Position,
			&item.ProblemID, &item.Slug, &item.LeetcodeID, &item.Title, &diff, &item.URL, &tags,
			&status, &item.NextDueAt, &item.IntervalDays, &item.Streak, &item.TotalAttempts, &item.TotalFails,
		); err != nil {
			return nil, fmt.Errorf("scan all leetcode problem: %w", err)
		}
		item.Difficulty = models.Difficulty(diff)
		item.Status = models.Status(status)
		if len(tags) > 0 {
			_ = json.Unmarshal(tags, &item.Topics)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
