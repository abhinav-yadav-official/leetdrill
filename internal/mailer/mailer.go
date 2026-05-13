// Package mailer sends transactional emails via SMTPS (port 465).
package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
)

// Mailer sends email via SMTPS.
type Mailer struct {
	host     string
	port     string
	user     string
	password string
	from     string
	appBase  string
}

// FromEnv constructs a Mailer from SMTP_* environment variables.
// appBase is the base URL used to build links (e.g. "https://leetdrill.example.com").
func FromEnv(appBase string) (*Mailer, error) {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil, errors.New("mailer: SMTP_HOST not set")
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "465"
	}
	user := os.Getenv("SMTP_USER")
	password := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = user
	}
	return &Mailer{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		from:     from,
		appBase:  strings.TrimRight(appBase, "/"),
	}, nil
}

// SendVerify sends an email verification link to addr.
func (m *Mailer) SendVerify(to, token string) error {
	link := m.appBase + "/verify?token=" + token
	subject := "Verify your LeetDrill email"
	body := fmt.Sprintf("Click the link below to verify your email address.\n\n%s\n\nThis link expires in 6 hours.\n", link)
	return m.send(to, subject, body)
}

// SendReset sends a password reset link to addr.
func (m *Mailer) SendReset(to, token string) error {
	link := m.appBase + "/reset?token=" + token
	subject := "Reset your LeetDrill password"
	body := fmt.Sprintf("Click the link below to reset your password.\n\n%s\n\nThis link expires in 6 hours. If you did not request a reset, ignore this email.\n", link)
	return m.send(to, subject, body)
}

func (m *Mailer) send(to, subject, body string) error {
	addr := net.JoinHostPort(m.host, m.port)
	tlsCfg := &tls.Config{ServerName: m.host}

	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("mailer: dial %s: %w", addr, err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return fmt.Errorf("mailer: new client: %w", err)
	}
	defer c.Close()

	if err := c.Auth(smtp.PlainAuth("", m.user, m.password, m.host)); err != nil {
		return fmt.Errorf("mailer: auth: %w", err)
	}
	if err := c.Mail(m.from); err != nil {
		return fmt.Errorf("mailer: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("mailer: RCPT TO: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("mailer: DATA: %w", err)
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		m.from, to, subject, body)
	if _, err := fmt.Fprint(wc, msg); err != nil {
		return fmt.Errorf("mailer: write: %w", err)
	}
	return wc.Close()
}
