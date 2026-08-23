package sync

import (
	"testing"
	"time"
)

func TestParseUnixSeconds(t *testing.T) {
	got, ok := parseUnixSeconds("1710000123")
	if !ok {
		t.Fatalf("parseUnixSeconds returned !ok")
	}
	if got.Unix() != 1710000123 {
		t.Fatalf("Unix() = %d", got.Unix())
	}
}

func TestColdStartDueAtSpreadsRecentProblems(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)

	first := coldStartDueAt(now, 0)
	last := coldStartDueAt(now, 19)

	if first.Before(now) || last.Before(now) {
		t.Fatalf("due dates must not be in the past")
	}
	if last.Sub(first) < 10*24*time.Hour {
		t.Fatalf("expected recent import spread, got %s", last.Sub(first))
	}
	if last.Sub(now) > 14*24*time.Hour {
		t.Fatalf("expected spread capped within 14d, got %s", last.Sub(now))
	}
}

func TestColdStartNeedsCookiesWhenPublicRecentIsEmpty(t *testing.T) {
	result := ColdStartResult{
		Username:      "almostturingcomplete",
		PublicSolved:  141,
		AuthedSkipped: true,
	}

	err := coldStartEmptyImportError(result)
	if err == nil {
		t.Fatalf("expected empty public recent import to require synced cookies")
	}
	if got := err.Error(); got != "cold-start: public profile has 141 solved problems but no public recent AC slugs; sync cookies for full history import" {
		t.Fatalf("error = %q", got)
	}
}

func TestColdStartSolvedDueAtSpreadsOlderProblems(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)

	first := coldStartSolvedDueAt(now, 0)
	last := coldStartSolvedDueAt(now, 140)

	if first.Before(now) || last.Before(now) {
		t.Fatalf("solved due dates must not be in the past")
	}
	if last.Sub(now) > 30*24*time.Hour {
		t.Fatalf("expected solved spread capped within 30d, got %s", last.Sub(now))
	}
}
