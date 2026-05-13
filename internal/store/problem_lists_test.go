package store

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestListCuratedListsCountsSolvedItems(t *testing.T) {
	db := &captureQueryDB{}

	_, err := ListCuratedLists(context.Background(), db, 7)
	if err != nil {
		t.Fatalf("ListCuratedLists() error = %v", err)
	}
	for _, want := range []string{
		"FROM problem_lists pl",
		"LEFT JOIN problem_list_items pli",
		"COUNT(pli.problem_id) AS total_items",
		"COUNT(pli.problem_id) FILTER (WHERE",
		"ORDER BY pl.sort_order ASC, pl.name ASC",
	} {
		if !strings.Contains(db.sql, want) {
			t.Fatalf("list query missing %q:\n%s", want, db.sql)
		}
	}
	if got := db.args; len(got) != 1 || got[0] != int64(7) {
		t.Fatalf("list args = %#v, want user id", got)
	}
}

func TestListCuratedListProblemsOrdersByListPosition(t *testing.T) {
	db := &captureQueryDB{}

	_, err := ListCuratedListProblems(context.Background(), db, 7, "blind-75")
	if err != nil {
		t.Fatalf("ListCuratedListProblems() error = %v", err)
	}
	for _, want := range []string{
		"FROM problem_list_items pli",
		"JOIN problem_lists pl",
		"pl.slug = $2",
		"ORDER BY pli.position ASC",
	} {
		if !strings.Contains(db.sql, want) {
			t.Fatalf("detail query missing %q:\n%s", want, db.sql)
		}
	}
	if strings.Contains(db.sql, "ORDER BY p.leetcode_frontend_id") {
		t.Fatalf("list detail must not use LeetCode number ordering:\n%s", db.sql)
	}
	if got := db.args; len(got) != 2 || got[0] != int64(7) || got[1] != "blind-75" {
		t.Fatalf("detail args = %#v, want user id and slug", got)
	}
}

func TestListAllLeetcodeProblemsOrdersByLeetcodeNumber(t *testing.T) {
	db := &captureQueryDB{}

	_, err := ListAllLeetcodeProblems(context.Background(), db, 7)
	if err != nil {
		t.Fatalf("ListAllLeetcodeProblems() error = %v", err)
	}
	for _, want := range []string{
		"ROW_NUMBER() OVER (ORDER BY p.leetcode_frontend_id::int ASC NULLS LAST, p.id ASC)::int AS position",
		"FROM problems p",
		"LEFT JOIN user_problems up",
		"ORDER BY p.leetcode_frontend_id::int ASC NULLS LAST, p.id ASC",
	} {
		if !strings.Contains(db.sql, want) {
			t.Fatalf("all list query missing %q:\n%s", want, db.sql)
		}
	}
	if strings.Contains(db.sql, "LIMIT") {
		t.Fatalf("all list query should not paginate:\n%s", db.sql)
	}
	if got := db.args; len(got) != 1 || got[0] != int64(7) {
		t.Fatalf("all list args = %#v, want user id", got)
	}
}

func TestAdditionalCuratedListMigrationSeedsPopularLists(t *testing.T) {
	body, err := os.ReadFile("../../migrations/00005_more_curated_lists.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	for _, want := range []string{
		"('neetcode-250', 'NeetCode 250'",
		"('striver-sde-sheet', 'Striver SDE Sheet'",
		"https://neetcode.io/practice/practice/neetcode250",
		"https://www.talentd.in/fleetcode/sheets/striver-sde-sheet",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}

	if got := countListItemRows(sql, "neetcode-250"); got != 250 {
		t.Fatalf("neetcode-250 seed rows = %d, want 250", got)
	}
	if got := countListItemRows(sql, "striver-sde-sheet"); got != 99 {
		t.Fatalf("striver-sde-sheet seed rows = %d, want 99", got)
	}
}

func TestFirstBatchCuratedListMigrationSeedsRequestedLists(t *testing.T) {
	body, err := os.ReadFile("../../migrations/00009_first_batch_lists.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	for _, want := range []string{
		"'leetcode-75'",
		"'LeetCode 75'",
		"'top-100-liked'",
		"'Top 100 Liked'",
		"'algomap-100'",
		"'AlgoMap 100'",
		"'sean-prashad-patterns'",
		"'Sean Prashad Patterns'",
		"'meta-top-50'",
		"'Meta Top 50'",
		"'microsoft-top-50'",
		"'Microsoft Top 50'",
		"https://leetcode.com/studyplan/leetcode-75/",
		"https://leetcode.com/studyplan/top-100-liked/",
		"https://algomap.io/roadmap",
		"https://seanprashad.com/leetcode-patterns/",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}

	for slug, want := range map[string]int{
		"leetcode-75":           75,
		"top-100-liked":         100,
		"algomap-100":           100,
		"sean-prashad-patterns": 178,
		"meta-top-50":           50,
		"microsoft-top-50":      50,
	} {
		if got := countListItemRows(sql, slug); got != want {
			t.Fatalf("%s seed rows = %d, want %d", slug, got, want)
		}
	}
}

func countListItemRows(sql, slug string) int {
	pattern := regexp.MustCompile(`\('` + regexp.QuoteMeta(slug) + `', +[0-9]+, '`)
	return len(pattern.FindAllString(sql, -1))
}
