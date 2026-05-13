package mailer_test

import (
	"os"
	"testing"

	"leetdrill/internal/mailer"
)

func TestFromEnvMissingHost(t *testing.T) {
	os.Unsetenv("SMTP_HOST")
	_, err := mailer.FromEnv("http://localhost:8080")
	if err == nil {
		t.Fatal("expected error when SMTP_HOST missing")
	}
}

func TestFromEnvOK(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "465")
	t.Setenv("SMTP_USER", "user@example.com")
	t.Setenv("SMTP_PASSWORD", "secret")
	t.Setenv("SMTP_FROM", "LeetDrill <noreply@example.com>")

	m, err := mailer.FromEnv("http://localhost:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil Mailer")
	}
}

func TestFromEnvDefaultPort(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_USER", "u")
	t.Setenv("SMTP_PASSWORD", "p")
	t.Setenv("SMTP_FROM", "LeetDrill <noreply@example.com>")

	_, err := mailer.FromEnv("http://localhost:8080")
	if err != nil {
		t.Fatalf("default port should work: %v", err)
	}
}
