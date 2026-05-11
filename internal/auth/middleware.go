package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"leetdrill/internal/store"
)

// CookieName is the web session cookie.
const CookieName = "ld_session"

// WebSessionDuration is how long a web cookie stays valid.
const WebSessionDuration = 30 * 24 * time.Hour

// ExtTokenDuration is how long an extension bearer stays valid.
const ExtTokenDuration = 365 * 24 * time.Hour

type ctxKey int

const userIDKey ctxKey = 1

// WithUserID stores the resolved user id in ctx.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserID returns the resolved user id, or 0 if absent.
func UserID(ctx context.Context) int64 {
	if v, ok := ctx.Value(userIDKey).(int64); ok {
		return v
	}
	return 0
}

// Authenticator wires middleware against a store.
type Authenticator struct {
	Store           *store.Store
	SingleUserID    int64 // non-zero enables single-user mode (no login)
	SecureCookies   bool  // set true behind TLS
}

// SetSessionCookie writes the ld_session cookie.
func (a *Authenticator) SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(WebSessionDuration),
		HttpOnly: true,
		Secure:   a.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie expires the cookie.
func (a *Authenticator) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   a.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// RequireWebSession enforces the web cookie. In single-user mode, any caller
// is logged in as SingleUserID.
func (a *Authenticator) RequireWebSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.SingleUserID != 0 {
			r = r.WithContext(WithUserID(r.Context(), a.SingleUserID))
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(CookieName)
		if err != nil || cookie.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		userID, err := a.lookup(r.Context(), store.AuthKindWeb, cookie.Value)
		if err != nil {
			a.ClearSessionCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))
	})
}

// RequireExtToken enforces Authorization: Bearer <token>. In single-user mode
// requests with no token are still allowed and treated as SingleUserID — so
// the extension can be configured trivially against a self-host backend.
// Tokenized requests are always validated against auth_sessions.
func (a *Authenticator) RequireExtToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r.Header.Get("Authorization"))
		if token == "" {
			if a.SingleUserID != 0 {
				next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), a.SingleUserID)))
				return
			}
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		userID, err := a.lookup(r.Context(), store.AuthKindExt, token)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))
	})
}

func extractBearer(h string) string {
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return ""
	}
	return strings.TrimSpace(h[len(p):])
}

func (a *Authenticator) lookup(ctx context.Context, kind store.AuthKind, token string) (int64, error) {
	if a.Store == nil {
		return 0, errors.New("auth: store not configured")
	}
	hash, err := HashToken(token)
	if err != nil {
		return 0, err
	}
	return store.LookupAuthSession(ctx, a.Store.DB(), kind, hash)
}

// IssueWebToken issues a fresh web token and stores it. Returns the raw token
// for setting in the cookie.
func (a *Authenticator) IssueWebToken(ctx context.Context, userID int64) (string, error) {
	return a.issue(ctx, userID, store.AuthKindWeb, WebSessionDuration)
}

// IssueExtToken issues a long-lived extension bearer.
func (a *Authenticator) IssueExtToken(ctx context.Context, userID int64) (string, error) {
	return a.issue(ctx, userID, store.AuthKindExt, ExtTokenDuration)
}

func (a *Authenticator) issue(ctx context.Context, userID int64, kind store.AuthKind, ttl time.Duration) (string, error) {
	token, hash, err := NewToken()
	if err != nil {
		return "", err
	}
	if _, err := store.InsertAuthSession(ctx, a.Store.DB(), userID, kind, hash, time.Now().Add(ttl)); err != nil {
		return "", err
	}
	return token, nil
}

// RevokeWebToken removes the row backing a given cookie.
func (a *Authenticator) RevokeWebToken(ctx context.Context, token string) error {
	hash, err := HashToken(token)
	if err != nil {
		return err
	}
	return store.DeleteAuthSession(ctx, a.Store.DB(), store.AuthKindWeb, hash)
}
