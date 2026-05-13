package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"leetdrill/internal/store"
)

func TestLoginPageUsesSplitIntroLayout(t *testing.T) {
	body := fmt.Sprintf(loginPage, "invalid email or password.", "/leetdrill/login", "/leetdrill/signup")

	for _, want := range []string{
		`<meta name="viewport" content="width=device-width, initial-scale=1">`,
		`Daily practice flow for LeetCode.`,
		`Track recent submissions, review timing, and difficult problems from one focused workspace.`,
		`invalid email or password.`,
		`action="/leetdrill/login"`,
		`href="/leetdrill/signup"`,
		`type="email"`,
		`type="password"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("login page missing %q:\n%s", want, body)
		}
	}
}

func TestSignupPageUsesSplitIntroLayout(t *testing.T) {
	body := fmt.Sprintf(signupPage, "create an account.", "/leetdrill/signup", "/leetdrill/login")

	for _, want := range []string{
		`<meta name="viewport" content="width=device-width, initial-scale=1">`,
		`Daily practice flow for LeetCode.`,
		`Create account`,
		`action="/leetdrill/signup"`,
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
