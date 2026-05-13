package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRendererLoadsCorePagesAndPartials(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}

	for _, name := range []string{
		"dashboard",
		"problems",
		"problem_detail",
		"patterns",
		"stats",
		"settings",
		"session_today",
	} {
		if _, ok := r.pages[name]; !ok {
			t.Fatalf("page %q not loaded", name)
		}
	}

	for _, name := range []string{"session_card", "problem_row"} {
		if _, ok := r.partials[name]; !ok {
			t.Fatalf("partial %q not loaded", name)
		}
	}
}

func TestRendererPageIncludesHTMXShell(t *testing.T) {
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}

	rec := httptest.NewRecorder()
	r.Page(rec, "dashboard", PageData{
		Title:   "Dashboard",
		UserID:  7,
		NavItem: "dashboard",
		Data:    map[string]string{"Now": "ok"},
	})

	body := rec.Body.String()
	for _, want := range []string{
		`<script src="https://unpkg.com/htmx.org`,
		`href="/session/today"`,
		`LeetDrill`,
		`Today`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered dashboard missing %q:\n%s", want, body)
		}
	}
}

func TestRendererPagePrefixesInternalLinks(t *testing.T) {
	r, err := NewRendererWithBasePath("/leetdrill")
	if err != nil {
		t.Fatalf("NewRendererWithBasePath() error = %v", err)
	}

	rec := httptest.NewRecorder()
	r.Page(rec, "dashboard", PageData{
		Title:   "Dashboard",
		UserID:  7,
		NavItem: "dashboard",
		Data:    map[string]string{"Now": "ok"},
	})

	body := rec.Body.String()
	for _, want := range []string{
		`href="/leetdrill/session/today"`,
		`action="/leetdrill/session/start"`,
		`href="/leetdrill/settings"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered dashboard missing prefixed URL %q:\n%s", want, body)
		}
	}
}

func TestRendererPageIncludesExtensionLink(t *testing.T) {
	r, err := NewRendererWithBasePath("/leetdrill")
	if err != nil {
		t.Fatalf("NewRendererWithBasePath() error = %v", err)
	}

	rec := httptest.NewRecorder()
	r.Page(rec, "dashboard", PageData{
		Title:   "Dashboard",
		UserID:  7,
		NavItem: "dashboard",
		Data:    map[string]string{"Now": "ok"},
	})

	body := rec.Body.String()
	for _, want := range []string{
		`href="https://abhiy.xyz/shared/leetdrill-extension/"`,
		`target="_blank"`,
		`Extension`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered dashboard missing extension link %q:\n%s", want, body)
		}
	}
}

func TestSettingsPageIncludesExtensionPanel(t *testing.T) {
	r, err := NewRendererWithBasePath("/leetdrill")
	if err != nil {
		t.Fatalf("NewRendererWithBasePath() error = %v", err)
	}

	rec := httptest.NewRecorder()
	r.Page(rec, "settings", PageData{
		Title:   "Settings",
		UserID:  7,
		NavItem: "settings",
		Data: map[string]string{
			"Username":        "abhinav-yadav-official",
			"CookieStatus":    "cookies stored and valid",
			"CookieUpdatedAt": "",
		},
	})

	body := rec.Body.String()
	for _, want := range []string{
		`Browser extension`,
		`href="https://abhiy.xyz/shared/leetdrill-extension/"`,
		`https://abhiy.xyz/leetdrill`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered settings missing extension panel %q:\n%s", want, body)
		}
	}
}

func TestProblemsPageIncludesPaginationControls(t *testing.T) {
	r, err := NewRendererWithBasePath("/leetdrill")
	if err != nil {
		t.Fatalf("NewRendererWithBasePath() error = %v", err)
	}

	rec := httptest.NewRecorder()
	r.Page(rec, "problems", PageData{
		Title:   "Problems",
		UserID:  7,
		NavItem: "problems",
		Data: map[string]any{
			"Filter":     "due",
			"Pattern":    "dynamic-programming",
			"Difficulty": "Medium",
			"Acceptance": "70",
			"AcceptanceBuckets": []map[string]string{{
				"Value": "70",
				"Label": "70-79%",
			}},
			"Patterns": []map[string]any{{
				"Slug": "dynamic-programming",
				"Name": "Dynamic Programming",
			}},
			"Problems": []map[string]any{{
				"Slug":          "unique-paths",
				"LeetcodeID":    "62",
				"Title":         "Unique Paths",
				"Difficulty":    "Medium",
				"Status":        "new",
				"Topics":        []any{},
				"TotalAttempts": 0,
			}},
			"Page":       2,
			"TotalPages": 4,
			"TotalCount": 327,
			"Start":      101,
			"End":        200,
			"PrevURL":    "/leetdrill/problems?acceptance=70&difficulty=Medium&filter=due&page=1&pattern=dynamic-programming",
			"NextURL":    "/leetdrill/problems?acceptance=70&difficulty=Medium&filter=due&page=3&pattern=dynamic-programming",
			"HasPrev":    true,
			"HasNext":    true,
		},
	})

	body := rec.Body.String()
	for _, want := range []string{
		`Page 2 of 4`,
		`101-200 of 327 problems`,
		`#62`,
		`Unique Paths`,
		`name="pattern"`,
		`value="dynamic-programming" selected`,
		`name="difficulty"`,
		`value="Medium" selected`,
		`name="acceptance"`,
		`value="70" selected`,
		`href="/leetdrill/problems?acceptance=70&amp;difficulty=Medium&amp;filter=due&amp;page=1&amp;pattern=dynamic-programming"`,
		`href="/leetdrill/problems?acceptance=70&amp;difficulty=Medium&amp;filter=due&amp;page=3&amp;pattern=dynamic-programming"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered problems missing pagination %q:\n%s", want, body)
		}
	}
}

func TestPatternsPageLinksToPatternFilteredProblems(t *testing.T) {
	r, err := NewRendererWithBasePath("/leetdrill")
	if err != nil {
		t.Fatalf("NewRendererWithBasePath() error = %v", err)
	}

	rec := httptest.NewRecorder()
	r.Page(rec, "patterns", PageData{
		Title:   "Patterns",
		UserID:  7,
		NavItem: "patterns",
		Data: map[string]any{
			"Patterns": []map[string]any{{
				"Slug":          "dynamic-programming",
				"Name":          "Dynamic Programming",
				"StrengthPct":   4,
				"CleanSolves":   29,
				"TotalProblems": 651,
				"Failures":      0,
			}},
		},
	})

	body := rec.Body.String()
	for _, want := range []string{
		`href="/leetdrill/problems?pattern=dynamic-programming"`,
		`29 solved · 651 problems`,
		`4%`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered patterns missing %q:\n%s", want, body)
		}
	}
}

func TestAppPath(t *testing.T) {
	tests := []struct {
		base string
		path string
		want string
	}{
		{base: "", path: "/", want: "/"},
		{base: "/leetdrill", path: "/", want: "/leetdrill/"},
		{base: "leetdrill/", path: "/login", want: "/leetdrill/login"},
		{base: "/", path: "/settings", want: "/settings"},
	}

	for _, tt := range tests {
		if got := AppPath(tt.base, tt.path); got != tt.want {
			t.Fatalf("AppPath(%q, %q) = %q, want %q", tt.base, tt.path, got, tt.want)
		}
	}
}
