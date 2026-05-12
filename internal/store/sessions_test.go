package store

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSessionNeedsProblemBuild(t *testing.T) {
	tests := []struct {
		name string
		s    Session
		want bool
	}{
		{
			name: "empty incomplete session rebuilds after ingest",
			s:    Session{},
			want: true,
		},
		{
			name: "session with problems is reused",
			s:    Session{ProblemIDs: []int64{1, 2, 3}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionNeedsProblemBuild(tt.s); got != tt.want {
				t.Fatalf("sessionNeedsProblemBuild() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReconcileSessionCompletionsUsesCapturedACAttempts(t *testing.T) {
	db := &captureExecDB{tag: pgconn.NewCommandTag("UPDATE 1")}

	changed, err := ReconcileSessionCompletions(context.Background(), db, 2, 7)
	if err != nil {
		t.Fatalf("ReconcileSessionCompletions() error = %v", err)
	}
	if !changed {
		t.Fatalf("ReconcileSessionCompletions() changed = false, want true")
	}
	if !strings.Contains(db.sql, "JOIN attempts a") {
		t.Fatalf("reconcile query must derive completions from attempts:\n%s", db.sql)
	}
	if !strings.Contains(db.sql, "a.verdict = 'AC'") {
		t.Fatalf("reconcile query must only mark AC attempts:\n%s", db.sql)
	}
	if got, want := db.args, []any{int64(2), int64(7)}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("reconcile args = %#v, want %#v", got, want)
	}
}

type captureExecDB struct {
	sql  string
	args []any
	tag  pgconn.CommandTag
}

func (db *captureExecDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.sql = sql
	db.args = args
	return db.tag, nil
}

func (db *captureExecDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("Query not implemented")
}

func (db *captureExecDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("QueryRow not implemented")
}
