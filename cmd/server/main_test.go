package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestLoginPageUsesSplitIntroLayout(t *testing.T) {
	body := fmt.Sprintf(loginPage, "invalid email or password.", "/leetdrill/login", "/leetdrill/signup")

	for _, want := range []string{
		`<meta name="viewport" content="width=device-width, initial-scale=1">`,
		`Daily review flow for LeetCode practice.`,
		`Track recent submissions, spaced repetition, and difficult problems from one focused workspace.`,
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
		`Daily review flow for LeetCode practice.`,
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
	got := problemPageURL("/leetdrill", "due", 2)
	want := "/leetdrill/problems?filter=due&page=2"
	if got != want {
		t.Fatalf("problemPageURL() = %q, want %q", got, want)
	}

	got = problemPageURL("", "", 1)
	want = "/problems?page=1"
	if got != want {
		t.Fatalf("problemPageURL() = %q, want %q", got, want)
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
