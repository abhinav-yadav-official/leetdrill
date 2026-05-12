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

	got, err := CountProblemsForUser(context.Background(), db, 7, "due")
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

type captureQueryRowDB struct {
	sql   string
	args  []any
	count int
}

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
