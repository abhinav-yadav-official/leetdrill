package store

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCountProblemsForUserUsesSameFilterJoin(t *testing.T) {
	db := &captureQueryRowDB{count: 123}

	got, err := CountProblemsForUser(context.Background(), db, 7, ProblemFilters{State: "due"})
	if err != nil {
		t.Fatalf("CountProblemsForUser() error = %v", err)
	}
	if got != 123 {
		t.Fatalf("CountProblemsForUser() = %d, want 123", got)
	}
	for _, want := range []string{
		"COUNT(*)",
		"FROM problems p",
		"LEFT JOIN user_problems up",
		"up.next_due_at IS NOT NULL",
		"up.status NOT IN ('leech','new','mastered')",
	} {
		if !strings.Contains(db.sql, want) {
			t.Fatalf("count query missing %q:\n%s", want, db.sql)
		}
	}
	if got := db.args; len(got) != 1 || got[0] != int64(7) {
		t.Fatalf("count args = %#v, want user id only", got)
	}
}

func TestCountProblemsForUserCanFilterByPattern(t *testing.T) {
	db := &captureQueryRowDB{count: 42}

	got, err := CountProblemsForUser(context.Background(), db, 7, ProblemFilters{
		State:   "review",
		Pattern: "dynamic-programming",
	})
	if err != nil {
		t.Fatalf("CountProblemsForUser() error = %v", err)
	}
	if got != 42 {
		t.Fatalf("CountProblemsForUser() = %d, want 42", got)
	}
	for _, want := range []string{
		"JOIN problem_patterns pp",
		"JOIN patterns pat",
		"pat.slug = $2",
		"up.status = 'review'",
	} {
		if !strings.Contains(db.sql, want) {
			t.Fatalf("pattern count query missing %q:\n%s", want, db.sql)
		}
	}
	if got := db.args; len(got) != 2 || got[0] != int64(7) || got[1] != "dynamic-programming" {
		t.Fatalf("count args = %#v, want user id and pattern slug", got)
	}
}

func TestListProblemsForUserCanFilterByPattern(t *testing.T) {
	db := &captureQueryDB{}

	_, err := ListProblemsForUser(context.Background(), db, 7, ProblemFilters{
		State:   "review",
		Pattern: "dynamic-programming",
	}, 100, 200)
	if err != nil {
		t.Fatalf("ListProblemsForUser() error = %v", err)
	}
	for _, want := range []string{
		"JOIN problem_patterns pp",
		"JOIN patterns pat",
		"pat.slug = $2",
		"up.status = 'review'",
		"LIMIT $3 OFFSET $4",
	} {
		if !strings.Contains(db.sql, want) {
			t.Fatalf("pattern list query missing %q:\n%s", want, db.sql)
		}
	}
	if got := db.args; len(got) != 4 || got[0] != int64(7) || got[1] != "dynamic-programming" || got[2] != 100 || got[3] != 200 {
		t.Fatalf("list args = %#v, want user id, pattern, limit, offset", got)
	}
}

func TestListProblemsForUserAppliesMetadataFiltersAndOrdersByLeetcodeNumber(t *testing.T) {
	db := &captureQueryDB{}

	_, err := ListProblemsForUser(context.Background(), db, 7, ProblemFilters{
		Pattern:    "dynamic-programming",
		Difficulty: "Medium",
		Acceptance: "70",
	}, 100, 200)
	if err != nil {
		t.Fatalf("ListProblemsForUser() error = %v", err)
	}
	for _, want := range []string{
		"pat.slug = $2",
		"p.difficulty = $3",
		"p.ac_rate >= $4 AND p.ac_rate < $5",
		"ORDER BY p.leetcode_frontend_id::int ASC NULLS LAST, p.id ASC",
		"LIMIT $6 OFFSET $7",
	} {
		if !strings.Contains(db.sql, want) {
			t.Fatalf("filtered list query missing %q:\n%s", want, db.sql)
		}
	}
	if got := db.args; len(got) != 7 ||
		got[0] != int64(7) ||
		got[1] != "dynamic-programming" ||
		got[2] != "Medium" ||
		got[3] != 70.0 ||
		got[4] != 80.0 ||
		got[5] != 100 ||
		got[6] != 200 {
		t.Fatalf("list args = %#v, want user id, pattern, difficulty, acceptance range, limit, offset", got)
	}
}

func TestCountProblemsForUserAppliesAcceptanceNinetiesBucket(t *testing.T) {
	db := &captureQueryRowDB{count: 9}

	_, err := CountProblemsForUser(context.Background(), db, 7, ProblemFilters{Acceptance: "90"})
	if err != nil {
		t.Fatalf("CountProblemsForUser() error = %v", err)
	}
	if !strings.Contains(db.sql, "p.ac_rate >= $2") {
		t.Fatalf("nineties bucket query missing lower-bound filter:\n%s", db.sql)
	}
	if strings.Contains(db.sql, "p.ac_rate <") {
		t.Fatalf("nineties bucket must include 100%% without upper bound:\n%s", db.sql)
	}
	if got := db.args; len(got) != 2 || got[0] != int64(7) || got[1] != 90.0 {
		t.Fatalf("count args = %#v, want user id and lower acceptance bound", got)
	}
}

func TestListPatternsWithStrengthUsesTotalProblemsAsDenominator(t *testing.T) {
	sql := patternsWithStrengthSQL()
	for _, want := range []string{
		"COUNT(DISTINCT pp.problem_id) AS total_problems",
		"COALESCE(COUNT(DISTINCT CASE WHEN up.clean_solves > 0 OR a.verdict = 'AC' THEN pp.problem_id END), 0) AS clean",
		"/ NULLIF(COUNT(DISTINCT pp.problem_id), 0)::int",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("pattern strength query missing %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "COUNT(a.id)") {
		t.Fatalf("pattern strength must not divide by attempts:\n%s", sql)
	}
}

type captureQueryRowDB struct {
	sql   string
	args  []any
	count int
}

type captureQueryDB struct {
	sql  string
	args []any
}

func (db *captureQueryDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("Exec not implemented")
}

func (db *captureQueryDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	db.sql = sql
	db.args = args
	return emptyRows{}, nil
}

func (db *captureQueryDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("QueryRow not implemented")
}

type emptyRows struct{}

func (emptyRows) Close()                                       {}
func (emptyRows) Err() error                                   { return nil }
func (emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyRows) Next() bool                                   { return false }
func (emptyRows) Scan(...any) error                            { return nil }
func (emptyRows) Values() ([]any, error)                       { return nil, nil }
func (emptyRows) RawValues() [][]byte                          { return nil }
func (emptyRows) Conn() *pgx.Conn                              { return nil }

func (db *captureQueryRowDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("Exec not implemented")
}

func (db *captureQueryRowDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("Query not implemented")
}

func (db *captureQueryRowDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	db.sql = sql
	db.args = args
	return countRow{count: db.count}
}

type countRow struct {
	count int
}

func (r countRow) Scan(dest ...any) error {
	*(dest[0].(*int)) = r.count
	return nil
}
