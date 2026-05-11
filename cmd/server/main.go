// Command server is the leetdrill web server entry point.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"leetdrill/internal/auth"
	"leetdrill/internal/leetcode"
	"leetdrill/internal/models"
	"leetdrill/internal/store"
	"leetdrill/internal/vault"
)

const maxReqBody = 256 * 1024 // 256 KB; submissions can include code

type server struct {
	addr     string
	store    *store.Store
	vault    *vault.Vault
	authmw   *auth.Authenticator
	tplLogin string
}

func main() {
	addr := envOr("LEETDRILL_ADDR", ":8080")
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL not set")
	}
	v, err := vault.FromEnv()
	if err != nil {
		log.Fatalf("vault: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st, err := store.Open(ctx, dsn)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	authmw := &auth.Authenticator{Store: st}

	// Single-user self-host mode: ensure the row exists and pin to it.
	if strings.EqualFold(os.Getenv("SINGLE_USER"), "true") {
		uid, err := store.EnsureSingleUser(ctx, st.DB(), os.Getenv("USER_EMAIL"), os.Getenv("LEETCODE_USERNAME"))
		if err != nil {
			log.Fatalf("ensure single user: %v", err)
		}
		authmw.SingleUserID = uid
		log.Printf("single-user mode: user_id=%d", uid)
	}

	srv := &server{
		addr:   addr,
		store:  st,
		vault:  v,
		authmw: authmw,
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutting down")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
}

func (s *server) router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/login", s.handleLoginPage)
	r.Post("/login", s.handleLoginSubmit)
	r.Post("/logout", s.handleLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.authmw.RequireWebSession)
		r.Get("/", s.handleHome)
		r.Get("/settings", s.handleSettings)
	})

	r.Route("/api/ext", func(r chi.Router) {
		// Handshake is the bootstrap path — no token required, but in
		// multi-user mode it demands basic-auth-style credentials in JSON.
		r.Post("/handshake", s.handleExtHandshake)

		r.Group(func(r chi.Router) {
			r.Use(s.authmw.RequireExtToken)
			r.Post("/cookies", s.handleExtCookies)
			r.Post("/submission", s.handleExtSubmission)
			r.Get("/next-problem", s.handleExtNextProblem)
		})
	})

	return r
}

// ---- HTML ----

const loginPage = `<!doctype html>
<html><head><meta charset="utf-8"><title>leetdrill — login</title></head>
<body style="font-family: system-ui; max-width: 480px; margin: 4rem auto;">
<h1>leetdrill</h1>
<p>%s</p>
<form method="post" action="/login">
  <p><label>Email <input type="email" name="email" autofocus required></label></p>
  <p><label>Password <input type="password" name="password" required></label></p>
  <p><button type="submit">log in</button></p>
</form>
</body></html>`

func (s *server) handleLoginPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, loginPage, "sign in to continue.")
}

func (s *server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	pw := r.FormValue("password")

	var (
		userID int64
		hash   string
	)
	const q = `SELECT id, password_hash FROM users WHERE email = $1`
	if err := s.store.DB().QueryRow(r.Context(), q, email).Scan(&userID, &hash); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, loginPage, "invalid email or password.")
		return
	}
	if err := auth.VerifyPassword(hash, pw); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, loginPage, "invalid email or password.")
		return
	}
	token, err := s.authmw.IssueWebToken(r.Context(), userID)
	if err != nil {
		http.Error(w, "issue token", http.StatusInternalServerError)
		return
	}
	s.authmw.SetSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.authmw.RevokeWebToken(r.Context(), c.Value)
	}
	s.authmw.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html><html><body style="font-family:system-ui"><h1>leetdrill</h1><p>logged in as user_id=%d</p><p>phase 4 wires real dashboard.</p><form method="post" action="/logout"><button>log out</button></form></body></html>`, uid)
}

func (s *server) handleSettings(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	c, err := store.GetLeetcodeCookies(r.Context(), s.store.DB(), uid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	status := "no cookies stored"
	if len(c.SessionEnc) > 0 {
		if c.Valid {
			status = "cookies stored and valid"
		} else {
			status = "cookies stored but marked invalid — re-sync via extension"
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html><html><body style="font-family:system-ui"><h1>settings</h1><p>user_id=%d, leetcode_username=%q</p><p>%s</p></body></html>`, uid, c.Username, status)
}

// ---- Extension API ----

type handshakeReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type handshakeResp struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in_seconds"`
}

func (s *server) handleExtHandshake(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	// Single-user mode: anyone hitting handshake gets the long-lived ext token.
	// Acceptable because the deployment is private; for multi-user, creds are
	// required.
	if s.authmw.SingleUserID != 0 {
		tok, err := s.authmw.IssueExtToken(r.Context(), s.authmw.SingleUserID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, handshakeResp{Token: tok, ExpiresIn: int(auth.ExtTokenDuration.Seconds())})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxReqBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req handshakeReq
	if err := json.Unmarshal(body, &req); err != nil || req.Email == "" || req.Password == "" {
		http.Error(w, "expected {email,password}", http.StatusBadRequest)
		return
	}
	var (
		userID int64
		hash   string
	)
	const q = `SELECT id, password_hash FROM users WHERE email = $1`
	if err := s.store.DB().QueryRow(r.Context(), q, req.Email).Scan(&userID, &hash); err != nil {
		http.Error(w, "bad credentials", http.StatusUnauthorized)
		return
	}
	if err := auth.VerifyPassword(hash, req.Password); err != nil {
		http.Error(w, "bad credentials", http.StatusUnauthorized)
		return
	}
	tok, err := s.authmw.IssueExtToken(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, handshakeResp{Token: tok, ExpiresIn: int(auth.ExtTokenDuration.Seconds())})
}

type cookiesReq struct {
	LeetcodeSession string `json:"leetcode_session"`
	CSRFToken       string `json:"csrf_token"`
}

func (s *server) handleExtCookies(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxReqBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req cookiesReq
	if err := json.Unmarshal(body, &req); err != nil || req.LeetcodeSession == "" || req.CSRFToken == "" {
		http.Error(w, "expected {leetcode_session,csrf_token}", http.StatusBadRequest)
		return
	}
	uid := auth.UserID(r.Context())

	sessEnc, err := s.vault.Seal([]byte(req.LeetcodeSession))
	if err != nil {
		http.Error(w, "seal session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	csrfEnc, err := s.vault.Seal([]byte(req.CSRFToken))
	if err != nil {
		http.Error(w, "seal csrf: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := store.SetLeetcodeCookies(r.Context(), s.store.DB(), uid, sessEnc, csrfEnc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type submissionReq struct {
	Slug             string `json:"slug"`
	Verdict          string `json:"verdict"`          // human form: "Accepted", "Wrong Answer", ...
	SubmissionCount  int    `json:"submission_count"` // submissions in this session up to and including this one
	TimeTakenSec     int    `json:"time_taken_sec"`   // wall-clock since page-open
	RuntimeMs        *int   `json:"runtime_ms,omitempty"`
	MemoryKB         *int   `json:"memory_kb,omitempty"`
	Language         string `json:"language,omitempty"`
	Code             string `json:"code,omitempty"`
	LeetcodeSubmID   string `json:"leetcode_submission_id,omitempty"`
	StartedAtUnix    int64  `json:"started_at_unix,omitempty"`
	CompletedAtUnix  int64  `json:"completed_at_unix,omitempty"`
}

type submissionResp struct {
	AttemptID   int64  `json:"attempt_id"`
	Rating      string `json:"derived_rating"`
	Status      string `json:"status"`
	NextDueAt   string `json:"next_due_at,omitempty"`
	IntervalDay int    `json:"interval_days"`
}

func (s *server) handleExtSubmission(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxReqBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req submissionReq
	if err := json.Unmarshal(body, &req); err != nil || req.Slug == "" || req.Verdict == "" {
		http.Error(w, "expected {slug, verdict, submission_count, time_taken_sec}", http.StatusBadRequest)
		return
	}
	if req.SubmissionCount < 1 {
		req.SubmissionCount = 1
	}

	uid := auth.UserID(r.Context())
	problem, err := store.GetProblemBySlug(r.Context(), s.store.DB(), req.Slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "unknown slug — run ingest first", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	verdict := leetcode.NormalizeVerdict(req.Verdict)

	in := store.ApplyInput{
		UserID:          uid,
		ProblemID:       problem.ID,
		Difficulty:      problem.Difficulty,
		Verdict:         verdict,
		SubmissionCount: req.SubmissionCount,
		TimeTakenSec:    req.TimeTakenSec,
		RuntimeMs:       req.RuntimeMs,
		MemoryKB:        req.MemoryKB,
		Language:        req.Language,
		Code:            req.Code,
		LeetcodeSubmID:  req.LeetcodeSubmID,
		StartedAt:       unixOrZero(req.StartedAtUnix),
		CompletedAt:     unixOrZero(req.CompletedAtUnix),
	}
	res, err := s.store.Apply(r.Context(), in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := submissionResp{
		AttemptID:   res.AttemptID,
		Rating:      string(res.Rating),
		Status:      string(res.UserProblem.Status),
		IntervalDay: res.UserProblem.IntervalDays,
	}
	if res.UserProblem.NextDueAt != nil {
		resp.NextDueAt = res.UserProblem.NextDueAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

type nextProblemResp struct {
	Slug       string             `json:"slug"`
	URL        string             `json:"url"`
	Title      string             `json:"title"`
	Difficulty models.Difficulty  `json:"difficulty"`
	Reason     string             `json:"reason"`
}

func (s *server) handleExtNextProblem(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	np, err := store.SelectNextDue(r.Context(), s.store.DB(), uid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "no problems available — run ingest", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, nextProblemResp{
		Slug:       np.Slug,
		URL:        np.URL,
		Title:      np.Title,
		Difficulty: np.Difficulty,
		Reason:     np.Reason,
	})
}

func unixOrZero(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0).UTC()
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
