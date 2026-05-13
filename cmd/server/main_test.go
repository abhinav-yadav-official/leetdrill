package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"leetdrill/internal/models"
	"leetdrill/internal/store"
)

func TestLoginPageUsesSplitIntroLayout(t *testing.T) {
	body := fmt.Sprintf(loginPage, "invalid email or password.", "/leetdrill/auth/google/start?next=%2Fleetdrill%2Fextension%2Fconnect", "/leetdrill/login", "/leetdrill/extension/connect", "/leetdrill/forgot", "/leetdrill/signup")

	for _, want := range []string{
		`href="favicon.svg"`,
		`aria-label="LeetDrill logo"`,
		`>LD</text>`,
		`<meta name="viewport" content="width=device-width, initial-scale=1">`,
		`Daily practice flow for LeetCode.`,
		`Track recent submissions, review timing, and difficult problems from one focused workspace.`,
		`invalid email or password.`,
		`action="/leetdrill/login"`,
		`name="next" value="/leetdrill/extension/connect"`,
		`href="/leetdrill/signup"`,
		`href="/leetdrill/auth/google/start?next=%2Fleetdrill%2Fextension%2Fconnect"`,
		`Continue with Google`,
		`leetdrill-theme`,
		`.dark .bg-white`,
		`type="email"`,
		`type="password"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("login page missing %q:\n%s", want, body)
		}
	}
}

func TestFaviconServesLDLogo(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)

	handleFavicon(w, req)

	if got := w.Result().Header.Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("Content-Type = %q, want image/svg+xml", got)
	}
	body := w.Body.String()
	for _, want := range []string{
		`<svg`,
		`aria-label="LeetDrill logo"`,
		`>LD</text>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("favicon missing %q:\n%s", want, body)
		}
	}
}

func TestSafeLoginNext(t *testing.T) {
	s := &server{basePath: "/leetdrill"}
	tests := []struct {
		raw  string
		want string
	}{
		{"", "/leetdrill/"},
		{"/leetdrill/extension/connect", "/leetdrill/extension/connect"},
		{"/leetdrill/problems?page=2", "/leetdrill/problems?page=2"},
		{"/other", "/leetdrill/"},
		{"https://evil.example/path", "/leetdrill/"},
		{"//evil.example/path", "/leetdrill/"},
	}

	for _, tt := range tests {
		if got := s.safeLoginNext(tt.raw); got != tt.want {
			t.Fatalf("safeLoginNext(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestSignupPageUsesSplitIntroLayout(t *testing.T) {
	body := fmt.Sprintf(signupPage, "create an account.", "/leetdrill/auth/google/start", "/leetdrill/signup", "/leetdrill/login")

	for _, want := range []string{
		`<meta name="viewport" content="width=device-width, initial-scale=1">`,
		`Daily practice flow for LeetCode.`,
		`Create account`,
		`action="/leetdrill/signup"`,
		`href="/leetdrill/auth/google/start"`,
		`Continue with Google`,
		`leetdrill-theme`,
		`.dark .bg-white`,
		`href="/leetdrill/login"`,
		`name="email"`,
		`name="password"`,
		`name="confirm_password"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("signup page missing %q:\n%s", want, body)
		}
	}
}

func TestGoogleOAuthStateRoundTrip(t *testing.T) {
	state, cookieValue, err := newGoogleOAuthState("/leetdrill/problems?page=2")
	if err != nil {
		t.Fatalf("newGoogleOAuthState() error = %v", err)
	}

	next, ok := parseGoogleOAuthState(state, cookieValue)
	if !ok {
		t.Fatalf("parseGoogleOAuthState() ok = false")
	}
	if next != "/leetdrill/problems?page=2" {
		t.Fatalf("next = %q", next)
	}
}

func TestGoogleOAuthStateRejectsMismatch(t *testing.T) {
	_, cookieValue, err := newGoogleOAuthState("/leetdrill/")
	if err != nil {
		t.Fatalf("newGoogleOAuthState() error = %v", err)
	}

	if _, ok := parseGoogleOAuthState("tampered", cookieValue); ok {
		t.Fatalf("parseGoogleOAuthState() ok = true, want false")
	}
}

func TestSetGoogleOAuthStateCookieUsesBasePath(t *testing.T) {
	s := &server{basePath: "/leetdrill"}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)

	s.setGoogleOAuthStateCookie(w, req, "state-cookie")

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != googleOAuthStateCookie || c.Path != "/leetdrill" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %#v", c)
	}
}

func TestValidateSignupForm(t *testing.T) {
	tests := []struct {
		name        string
		email       string
		password    string
		confirm     string
		wantEmail   string
		wantMessage string
	}{
		{
			name:      "valid normalized email",
			email:     "  USER@example.COM ",
			password:  "correct horse",
			confirm:   "correct horse",
			wantEmail: "user@example.com",
		},
		{
			name:        "email required",
			password:    "correct horse",
			confirm:     "correct horse",
			wantMessage: "email is required.",
		},
		{
			name:        "password length",
			email:       "user@example.com",
			password:    "short",
			confirm:     "short",
			wantMessage: "password must be at least 8 characters.",
		},
		{
			name:        "confirmation mismatch",
			email:       "user@example.com",
			password:    "correct horse",
			confirm:     "wrong horse",
			wantMessage: "passwords do not match.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEmail, gotMessage := validateSignupForm(tt.email, tt.password, tt.confirm)
			if gotEmail != tt.wantEmail || gotMessage != tt.wantMessage {
				t.Fatalf("validateSignupForm() = (%q, %q), want (%q, %q)", gotEmail, gotMessage, tt.wantEmail, tt.wantMessage)
			}
		})
	}
}

func TestParsePage(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"", 1},
		{"0", 1},
		{"-2", 1},
		{"abc", 1},
		{"3", 3},
	}

	for _, tt := range tests {
		if got := parsePage(tt.raw); got != tt.want {
			t.Fatalf("parsePage(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

func TestProblemPageURLPreservesFilterAndBasePath(t *testing.T) {
	got := problemPageURL("/leetdrill", store.ProblemFilters{
		State:      "due",
		Pattern:    "dynamic-programming",
		Difficulty: "Medium",
		Acceptance: "70",
	}, 2)
	want := "/leetdrill/problems?acceptance=70&difficulty=Medium&filter=due&page=2&pattern=dynamic-programming"
	if got != want {
		t.Fatalf("problemPageURL() = %q, want %q", got, want)
	}

	got = problemPageURL("", store.ProblemFilters{}, 1)
	want = "/problems?page=1"
	if got != want {
		t.Fatalf("problemPageURL() = %q, want %q", got, want)
	}
}

func TestSessionPollURLPreservesTodayFilter(t *testing.T) {
	got := sessionPollURL("/leetdrill", 42, "not-solved")
	want := "/leetdrill/session/42/next?filter=not-solved"
	if got != want {
		t.Fatalf("sessionPollURL() = %q, want %q", got, want)
	}

	got = sessionPollURL("", 42, "")
	want = "/session/42/next"
	if got != want {
		t.Fatalf("sessionPollURL() = %q, want %q", got, want)
	}
}

func TestNormalizeCompletionFilter(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"", ""},
		{"solved", "solved"},
		{"not-solved", "not-solved"},
		{"unsolved", "not-solved"},
		{"review", ""},
	}

	for _, tt := range tests {
		if got := normalizeCompletionFilter(tt.raw); got != tt.want {
			t.Fatalf("normalizeCompletionFilter(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestNormalizeMistakeTagsKeepsKnownTagsInTaxonomyOrder(t *testing.T) {
	got := normalizeMistakeTags([]string{
		" complexity ",
		"unknown",
		"edge-case",
		"complexity",
		"off-by-one",
	})
	want := []string{"edge-case", "off-by-one", "complexity"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeMistakeTags() = %#v, want %#v", got, want)
	}
}

func TestSortSessionProblemsByLeetcodeNumber(t *testing.T) {
	problems := []sessionProblem{
		{Title: "Ten", LeetcodeID: "10"},
		{Title: "Two", LeetcodeID: "2"},
		{Title: "No ID"},
		{Title: "One", LeetcodeID: "1"},
	}

	sortSessionProblems(problems)

	got := []string{problems[0].Title, problems[1].Title, problems[2].Title, problems[3].Title}
	want := []string{"One", "Two", "Ten", "No ID"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted titles = %#v, want %#v", got, want)
		}
	}
}

func TestHandshakeRequestSupportsWebSessionToken(t *testing.T) {
	body := `{"web_session_token":"web-token"}`
	var req handshakeReq
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if req.WebSessionToken != "web-token" {
		t.Fatalf("WebSessionToken = %q, want web-token", req.WebSessionToken)
	}
}

func TestExtensionConnectPageCarriesTokenForContentScript(t *testing.T) {
	body := renderExtensionConnectPage("ext-token")

	for _, want := range []string{
		`<meta name="leetdrill-extension-token" content="ext-token">`,
		`id="manual_token"`,
		`ext-token`,
		`LeetDrill extension connected`,
		`LEETDRILL_WEB_CONNECT_TOKEN`,
		`LEETDRILL_WEB_CONNECT_DONE`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("extension connect page missing %q:\n%s", want, body)
		}
	}
}

func TestExtTodayProblemResponseFromSessionCard(t *testing.T) {
	card := sessionCardData{
		Problems: []sessionProblem{
			{
				Slug:       "unique-paths",
				LeetcodeID: "62",
				Title:      "Unique Paths",
				Difficulty: models.DifficultyMedium,
				URL:        "https://leetcode.com/problems/unique-paths/",
				Completed:  true,
			},
		},
		CompletedCount: 1,
		TotalCount:     1,
		Done:           true,
	}

	resp := extTodayProblemResponseFromCard(card)
	if len(resp.Problems) != 1 {
		t.Fatalf("len(Problems) = %d, want 1", len(resp.Problems))
	}
	got := resp.Problems[0]
	if got.LeetcodeID != "62" || got.Title != "Unique Paths" || got.URL == "" || !got.Completed {
		t.Fatalf("problem = %#v", got)
	}
	if resp.CompletedCount != 1 || resp.TotalCount != 1 || !resp.Done {
		t.Fatalf("summary = %#v", resp)
	}
}

func TestTotalPages(t *testing.T) {
	tests := []struct {
		total    int
		pageSize int
		want     int
	}{
		{total: 0, pageSize: 100, want: 1},
		{total: 1, pageSize: 100, want: 1},
		{total: 100, pageSize: 100, want: 1},
		{total: 101, pageSize: 100, want: 2},
	}

	for _, tt := range tests {
		if got := totalPages(tt.total, tt.pageSize); got != tt.want {
			t.Fatalf("totalPages(%d, %d) = %d, want %d", tt.total, tt.pageSize, got, tt.want)
		}
	}
}
