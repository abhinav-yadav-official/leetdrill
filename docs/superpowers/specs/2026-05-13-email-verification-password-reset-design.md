# Email Verification & Password Reset

**Date:** 2026-05-13  
**Scope:** Multi-user mode only. Single-user mode (`SINGLE_USER=true`) bypasses all of this.

---

## Goals

1. Require email verification before app access (new signups blocked until verified).
2. Allow password reset via emailed link.
3. Mark all existing users as verified via migration (no disruption to current users).

---

## Database

### Migration `00002_email_verification.sql`

```sql
-- email_tokens: stores verify + reset tokens (hashed)
CREATE TABLE email_tokens (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('verify', 'reset')),
    token_hash BYTEA UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX email_tokens_user_kind_idx ON email_tokens (user_id, kind);

-- Add email_verified_at to users
ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;

-- Mark all existing users as verified (no disruption)
UPDATE users SET email_verified_at = now();
```

Token expiry: 6 hours for both verify and reset tokens.  
Consumed tokens: set `used_at = now()`, not deleted (audit trail).  
Old unused tokens for same user+kind are invalidated on new issue (set `used_at = now()` before insert).

---

## Packages

### `internal/mailer`

New package. Single file `mailer.go`.

```go
type Mailer struct {
    host, port, user, password, from string
    appBase string // for building links
}

func FromEnv(appBase string) (*Mailer, error)
func (m *Mailer) SendVerify(to, token string) error
func (m *Mailer) SendReset(to, token string) error
```

- Uses `net/smtp` (stdlib). TLS via `crypto/tls` for port 465 (implicit TLS / SMTPS).
- Reads `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` from env.
- Links: `{appBase}/verify?token={token}` and `{appBase}/reset?token={token}`.
- Plain-text emails only (no HTML email).

### `internal/store/emailtokens.go`

New file in existing `store` package.

```go
func (s *Store) CreateEmailToken(ctx, userID int64, kind string, hash []byte, expiresAt time.Time) error
func (s *Store) ConsumeEmailToken(ctx, hash []byte, kind string) (userID int64, err error)
func (s *Store) MarkEmailVerified(ctx, userID int64) error
func (s *Store) SetPasswordHash(ctx, userID int64, hash string) error
```

`ConsumeEmailToken`: finds token by hash+kind where `used_at IS NULL AND expires_at > now()`, sets `used_at`, returns `user_id`. Returns `store.ErrTokenInvalid` sentinel if not found/expired/used.

Before creating a new token, invalidate existing unused tokens of same user+kind:
```sql
UPDATE email_tokens SET used_at = now()
WHERE user_id = $1 AND kind = $2 AND used_at IS NULL
```

---

## Auth Middleware Change

`internal/auth/middleware.go` — `RequireWebSession` gains:

```go
// Skip in single-user mode
if a.SingleUserID != 0 {
    next.ServeHTTP(w, r.WithContext(...))
    return
}
// Check verified
if user.EmailVerifiedAt == nil {
    http.Redirect(w, r, a.BasePath+"/verify-pending", http.StatusFound)
    return
}
```

Session struct gains `EmailVerifiedAt *time.Time` field (loaded from `auth_sessions` join or separate query on `users`).

---

## Routes

All new routes added in `cmd/server/main.go`:

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| GET | `/verify-pending` | `handleVerifyPending` | none |
| GET | `/verify` | `handleVerifyEmail` | none |
| GET | `/forgot` | `handleForgotPage` | none |
| POST | `/forgot` | `handleForgotSubmit` | none |
| GET | `/reset` | `handleResetPage` | none |
| POST | `/reset` | `handleResetSubmit` | none |
| POST | `/resend-verify` | `handleResendVerify` | none |

---

## Flow: Signup

1. `POST /signup` — validate, hash password, insert user (`email_verified_at` NULL).
2. Generate verify token via `auth.NewToken()`, store hash in `email_tokens`.
3. `mailer.SendVerify(email, token)` — on error, log but don't fail signup.
4. Redirect to `/verify-pending?email={encoded_email}`.

---

## Flow: Verify Email

1. `GET /verify?token=...` — call `store.ConsumeEmailToken(hash, "verify")`.
2. On success: `store.MarkEmailVerified(userID)`, show `verifyDonePage` with link to `/login`.
3. On failure (invalid/expired/used): show `verifyDonePage` with error message + link to `/verify-pending`.

---

## Flow: Forgot Password

1. `GET /forgot` — show `forgotPage`.
2. `POST /forgot` — look up user by email.
   - If not found: show same success message (no user enumeration).
   - If found: invalidate old reset tokens, generate new token, `mailer.SendReset(email, token)`.
   - Always redirect to `/forgot` with `?sent=1` (success message regardless).

---

## Flow: Reset Password

1. `GET /reset?token=...` — validate token exists (don't consume yet), show `resetPage` with token in hidden field.
2. `POST /reset` — consume token via `store.ConsumeEmailToken(hash, "reset")`.
   - On failure: show error, link to `/forgot`.
   - On success: `store.SetPasswordHash(userID, newHash)`, redirect to `/login?reset=1`.
3. Validate: password ≥ 8 chars, confirm matches.

---

## Flow: Resend Verify

`POST /resend-verify` — body field `email`.

Rate limit: in-memory `sync.Map` keyed by IP. Value: `[]time.Time` of recent requests. Allow 5 per hour per IP. Purge stale entries on check.

Steps:
1. Check rate limit → 429 if exceeded.
2. Look up user by email.
3. If user found and `email_verified_at IS NULL`: invalidate old verify tokens, generate new, send email.
4. Always return 200 (no user enumeration).
5. Redirect to `/verify-pending?email=...&resent=1`.

---

## UI Pages (inline const HTML)

All follow the same split-layout style as `loginPage` / `signupPage` (left marketing panel, right card).

| Const | Slots (`%s`) |
|-------|--------------|
| `verifyPendingPage` | error/success msg, email display, resend action URL, login URL |
| `forgotPage` | error/success msg, form action URL, login URL |
| `resetPage` | error msg, form action URL, token hidden value |
| `verifyDonePage` | heading, message, link URL, link text |

**Login page change:** Add one line — `Forgot password?` link between password field and submit button.

---

## Rate Limiting

```go
type ipRateLimiter struct {
    mu      sync.Mutex
    entries map[string][]time.Time
    limit   int
    window  time.Duration
}

func (l *ipRateLimiter) Allow(ip string) bool
```

Constructed once in `server` struct. Limit: 5 requests per hour per IP. No external dependency.

---

## Server Struct Changes

```go
type server struct {
    // existing fields...
    mailer  *mailer.Mailer   // nil in single-user mode
    resendLimiter *ipRateLimiter
}
```

`mailer.FromEnv` called in `main()`. If `SMTP_HOST` is empty and not single-user mode → log warning (app still starts, emails fail gracefully with log error).

---

## Out of Scope

- HTML emails
- Admin user management
- Email change flow
- Account deletion
- Invite links
