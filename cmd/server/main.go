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
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"leetdrill/internal/auth"
	"leetdrill/internal/leetcode"
	"leetdrill/internal/models"
	"leetdrill/internal/store"
	ldsync "leetdrill/internal/sync"
	"leetdrill/internal/vault"
	"leetdrill/internal/web"
)

const maxReqBody = 256 * 1024 // 256 KB; submissions can include code

type server struct {
	addr     string
	store    *store.Store
	vault    *vault.Vault
	authmw   *auth.Authenticator
	renderer *web.Renderer
	basePath string
}

func main() {
	addr := envOr("LEETDRILL_ADDR", ":8080")
	basePath := web.CleanBasePath(os.Getenv("LEETDRILL_BASE_PATH"))
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

	authmw := &auth.Authenticator{
		Store:         st,
		BasePath:      basePath,
		SecureCookies: strings.EqualFold(os.Getenv("LEETDRILL_SECURE_COOKIES"), "true"),
	}
	renderer, err := web.NewRendererWithBasePath(basePath)
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

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
		addr:     addr,
		store:    st,
		vault:    v,
		authmw:   authmw,
		renderer: renderer,
		basePath: basePath,
	}
	if !strings.EqualFold(os.Getenv("LEETDRILL_SYNC_WORKER"), "false") {
		(&ldsync.RecentWorker{
			Store:    st,
			Client:   leetcode.New(),
			Interval: 30 * time.Minute,
			Logger:   log.Default(),
		}).Start(ctx)
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
	r.Get("/signup", s.handleSignupPage)
	r.Post("/signup", s.handleSignupSubmit)
	r.Post("/logout", s.handleLogout)

	r.Group(func(r chi.Router) {
		r.Use(s.authmw.RequireWebSession)
		r.Get("/", s.handleHome)
		r.Post("/session/start", s.handleSessionStart)
		r.Get("/session/today", s.handleSessionToday)
		r.Get("/session/{id}/next", s.handleSessionNext)
		r.Get("/problems", s.handleProblems)
		r.Get("/problems/{slug}", s.handleProblemDetail)
		r.Post("/problems/{id}/journal", s.handleProblemJournal)
		r.Post("/problems/{id}/triage", s.handleProblemTriage)
		r.Get("/patterns", s.handlePatterns)
		r.Get("/stats", s.handleStats)
		r.Get("/settings", s.handleSettings)
		r.Post("/settings/cold-start", s.handleSettingsColdStart)
		r.Post("/settings/vacation", s.handleSettingsVacation)
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
			r.Post("/cold-start", s.handleExtColdStart)
		})
	})

	return r
}

func (s *server) page(w http.ResponseWriter, name string, p web.PageData) {
	p.BasePath = s.basePath
	s.renderer.Page(w, name, p)
}

func (s *server) appPath(target string) string {
	return web.AppPath(s.basePath, target)
}

// ---- HTML ----

const loginPage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Login · LeetDrill</title>
    <script src="https://cdn.tailwindcss.com"></script>
  </head>
  <body class="min-h-screen bg-zinc-50 text-zinc-950">
    <main class="mx-auto grid min-h-screen max-w-6xl items-center gap-10 px-4 py-10 sm:px-6 lg:grid-cols-[1fr_420px] lg:px-8">
      <section class="max-w-xl">
        <div class="text-sm font-semibold uppercase tracking-normal text-zinc-500">LeetDrill</div>
        <h1 class="mt-3 text-3xl font-semibold tracking-normal text-zinc-950 sm:text-4xl">Daily practice flow for LeetCode.</h1>
        <p class="mt-4 max-w-lg text-base leading-7 text-zinc-600">Track recent submissions, review timing, and difficult problems from one focused workspace.</p>
        <div class="mt-8 grid max-w-md grid-cols-3 gap-3 text-sm">
          <div class="rounded-lg border border-zinc-200 bg-white p-3">
            <div class="text-xs font-medium uppercase tracking-normal text-zinc-500">Queue</div>
            <div class="mt-2 font-semibold text-zinc-900">Due first</div>
          </div>
          <div class="rounded-lg border border-zinc-200 bg-white p-3">
            <div class="text-xs font-medium uppercase tracking-normal text-zinc-500">Signal</div>
            <div class="mt-2 font-semibold text-zinc-900">Attempts</div>
          </div>
          <div class="rounded-lg border border-zinc-200 bg-white p-3">
            <div class="text-xs font-medium uppercase tracking-normal text-zinc-500">Plan</div>
            <div class="mt-2 font-semibold text-zinc-900">Review plan</div>
          </div>
        </div>
      </section>

      <section class="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm">
        <div>
          <h2 class="text-xl font-semibold tracking-normal">Sign in</h2>
          <p class="mt-2 text-sm text-zinc-600" aria-live="polite">%s</p>
        </div>
        <form class="mt-6 space-y-4" method="post" action="%s">
          <div>
            <label class="block text-sm font-medium text-zinc-700" for="email">Email</label>
            <input id="email" class="mt-2 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-2 focus:ring-zinc-900/10" type="email" name="email" autocomplete="email" autofocus required>
          </div>
          <div>
            <label class="block text-sm font-medium text-zinc-700" for="password">Password</label>
            <input id="password" class="mt-2 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-2 focus:ring-zinc-900/10" type="password" name="password" autocomplete="current-password" required>
          </div>
          <button class="w-full rounded-md bg-zinc-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-zinc-800 focus:outline-none focus:ring-2 focus:ring-zinc-900 focus:ring-offset-2" type="submit">Log in</button>
        </form>
        <p class="mt-5 text-center text-sm text-zinc-600">New here? <a class="font-medium text-zinc-950 underline" href="%s">Create an account</a></p>
      </section>
    </main>
  </body>
</html>`

const signupPage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Sign up · LeetDrill</title>
    <script src="https://cdn.tailwindcss.com"></script>
  </head>
  <body class="min-h-screen bg-zinc-50 text-zinc-950">
    <main class="mx-auto grid min-h-screen max-w-6xl items-center gap-10 px-4 py-10 sm:px-6 lg:grid-cols-[1fr_420px] lg:px-8">
      <section class="max-w-xl">
        <div class="text-sm font-semibold uppercase tracking-normal text-zinc-500">LeetDrill</div>
        <h1 class="mt-3 text-3xl font-semibold tracking-normal text-zinc-950 sm:text-4xl">Daily practice flow for LeetCode.</h1>
        <p class="mt-4 max-w-lg text-base leading-7 text-zinc-600">Track recent submissions, review timing, and difficult problems from one focused workspace.</p>
        <div class="mt-8 grid max-w-md grid-cols-3 gap-3 text-sm">
          <div class="rounded-lg border border-zinc-200 bg-white p-3">
            <div class="text-xs font-medium uppercase tracking-normal text-zinc-500">Queue</div>
            <div class="mt-2 font-semibold text-zinc-900">Due first</div>
          </div>
          <div class="rounded-lg border border-zinc-200 bg-white p-3">
            <div class="text-xs font-medium uppercase tracking-normal text-zinc-500">Signal</div>
            <div class="mt-2 font-semibold text-zinc-900">Attempts</div>
          </div>
          <div class="rounded-lg border border-zinc-200 bg-white p-3">
            <div class="text-xs font-medium uppercase tracking-normal text-zinc-500">Plan</div>
            <div class="mt-2 font-semibold text-zinc-900">Review plan</div>
          </div>
        </div>
      </section>

      <section class="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm">
        <div>
          <h2 class="text-xl font-semibold tracking-normal">Create account</h2>
          <p class="mt-2 text-sm text-zinc-600" aria-live="polite">%s</p>
        </div>
        <form class="mt-6 space-y-4" method="post" action="%s">
          <div>
            <label class="block text-sm font-medium text-zinc-700" for="email">Email</label>
            <input id="email" class="mt-2 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-2 focus:ring-zinc-900/10" type="email" name="email" autocomplete="email" autofocus required>
          </div>
          <div>
            <label class="block text-sm font-medium text-zinc-700" for="password">Password</label>
            <input id="password" class="mt-2 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-2 focus:ring-zinc-900/10" type="password" name="password" autocomplete="new-password" minlength="8" required>
          </div>
          <div>
            <label class="block text-sm font-medium text-zinc-700" for="confirm_password">Confirm password</label>
            <input id="confirm_password" class="mt-2 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-2 focus:ring-zinc-900/10" type="password" name="confirm_password" autocomplete="new-password" minlength="8" required>
          </div>
          <button class="w-full rounded-md bg-zinc-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-zinc-800 focus:outline-none focus:ring-2 focus:ring-zinc-900 focus:ring-offset-2" type="submit">Sign up</button>
        </form>
        <p class="mt-5 text-center text-sm text-zinc-600">Already have an account? <a class="font-medium text-zinc-950 underline" href="%s">Log in</a></p>
      </section>
    </main>
  </body>
</html>`

func (s *server) handleLoginPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, loginPage, "sign in to continue.", s.appPath("/login"), s.appPath("/signup"))
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
		_, _ = fmt.Fprintf(w, loginPage, "invalid email or password.", s.appPath("/login"), s.appPath("/signup"))
		return
	}
	if err := auth.VerifyPassword(hash, pw); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, loginPage, "invalid email or password.", s.appPath("/login"), s.appPath("/signup"))
		return
	}
	token, err := s.authmw.IssueWebToken(r.Context(), userID)
	if err != nil {
		http.Error(w, "issue token", http.StatusInternalServerError)
		return
	}
	s.authmw.SetSessionCookie(w, token)
	http.Redirect(w, r, s.appPath("/"), http.StatusSeeOther)
}

func (s *server) handleSignupPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, signupPage, "create an account.", s.appPath("/signup"), s.appPath("/login"))
}

func (s *server) handleSignupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email, message := validateSignupForm(
		r.FormValue("email"),
		r.FormValue("password"),
		r.FormValue("confirm_password"),
	)
	if message != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, signupPage, message, s.appPath("/signup"), s.appPath("/login"))
		return
	}
	hash, err := auth.HashPassword(r.FormValue("password"))
	if err != nil {
		http.Error(w, "hash password", http.StatusInternalServerError)
		return
	}
	var userID int64
	err = s.store.DB().QueryRow(r.Context(),
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, hash,
	).Scan(&userID)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, signupPage, "could not create account.", s.appPath("/signup"), s.appPath("/login"))
		return
	}
	token, err := s.authmw.IssueWebToken(r.Context(), userID)
	if err != nil {
		http.Error(w, "issue token", http.StatusInternalServerError)
		return
	}
	s.authmw.SetSessionCookie(w, token)
	http.Redirect(w, r, s.appPath("/"), http.StatusSeeOther)
}

func validateSignupForm(email, password, confirm string) (string, string) {
	email = strings.ToLower(strings.TrimSpace(email))
	switch {
	case email == "":
		return "", "email is required."
	case len(password) < 8:
		return "", "password must be at least 8 characters."
	case password != confirm:
		return "", "passwords do not match."
	default:
		return email, ""
	}
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.authmw.RevokeWebToken(r.Context(), c.Value)
	}
	s.authmw.ClearSessionCookie(w)
	http.Redirect(w, r, s.appPath("/login"), http.StatusSeeOther)
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	counts, err := store.CountsForDashboard(r.Context(), s.store.DB(), uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	recent, err := store.RecentAttempts(r.Context(), s.store.DB(), uid, 10)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	streak, err := store.CurrentStreakDays(r.Context(), s.store.DB(), uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	vacation, err := store.GetVacationUntil(r.Context(), s.store.DB(), uid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.page(w, "dashboard", web.PageData{
		Title:   "Dashboard",
		UserID:  uid,
		NavItem: "dashboard",
		Data: dashboardPageData{
			Counts:         counts,
			RecentAttempts: recent,
			StreakDays:     streak,
			VacationUntil:  vacation,
		},
	})
}

type dashboardPageData struct {
	Counts         store.DashboardCounts
	RecentAttempts []store.RecentAttempt
	StreakDays     int
	VacationUntil  *time.Time
}

func (s *server) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	if _, err := store.EnsureTodaySession(r.Context(), s.store.DB(), uid, 5); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.appPath("/session/today"), http.StatusSeeOther)
}

func (s *server) handleSessionToday(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	filter := normalizeCompletionFilter(r.URL.Query().Get("filter"))
	sess, err := store.EnsureTodaySession(r.Context(), s.store.DB(), uid, 5)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if changed, err := store.ReconcileSessionCompletions(r.Context(), s.store.DB(), uid, sess.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if changed {
		sess, err = store.GetSession(r.Context(), s.store.DB(), uid, sess.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	card, err := s.sessionCard(r.Context(), uid, sess, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.page(w, "session_today", web.PageData{
		Title:   "Today",
		UserID:  uid,
		NavItem: "today",
		Data:    sessionPageData{Filter: filter, Card: card},
	})
}

type sessionPageData struct {
	Filter string
	Card   sessionCardData
}

func (s *server) handleSessionNext(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	sessionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || sessionID <= 0 {
		http.Error(w, "bad session id", http.StatusBadRequest)
		return
	}
	sess, err := store.GetSession(r.Context(), s.store.DB(), uid, sessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}

	if changed, err := store.ReconcileSessionCompletions(r.Context(), s.store.DB(), uid, sess.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if changed {
		sess, err = store.GetSession(r.Context(), s.store.DB(), uid, sessionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	filter := normalizeCompletionFilter(r.URL.Query().Get("filter"))
	card, err := s.sessionCard(r.Context(), uid, sess, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderer.Partial(w, "session_card", card)
}

type sessionCardData struct {
	Session        *store.Session
	Problems       []sessionProblem
	PollURL        string
	Done           bool
	CompletedCount int
	TotalCount     int
}

type sessionProblem struct {
	ProblemID  int64
	Slug       string
	LeetcodeID string
	Title      string
	Difficulty models.Difficulty
	URL        string
	Topics     []models.Tag
	Status     models.Status
	Completed  bool
	Journal    string
}

func (s *server) sessionCard(ctx context.Context, uid int64, sess *store.Session, filter string) (sessionCardData, error) {
	card := sessionCardData{
		Session:        sess,
		PollURL:        sessionPollURL(s.basePath, sess.ID, filter),
		CompletedCount: len(sess.CompletedProblemIDs),
		TotalCount:     len(sess.ProblemIDs),
	}

	completedMap := make(map[int64]bool)
	for _, id := range sess.CompletedProblemIDs {
		completedMap[id] = true
	}

	for _, pid := range sess.ProblemIDs {
		completed := completedMap[pid]
		if filter == "solved" && !completed {
			continue
		}
		if filter == "not-solved" && completed {
			continue
		}

		p, err := store.GetProblemByID(ctx, s.store.DB(), pid)
		if err != nil {
			return card, err
		}
		up, err := store.GetUserProblem(ctx, s.store.DB(), uid, pid)
		if err != nil {
			return card, err
		}

		journal := ""
		detail, err := store.GetProblemDetail(ctx, s.store.DB(), uid, p.LeetcodeSlug, 1)
		if err == nil && len(detail.Attempts) > 0 {
			journal = detail.Attempts[0].Journal
		}

		card.Problems = append(card.Problems, sessionProblem{
			ProblemID:  p.ID,
			Slug:       p.LeetcodeSlug,
			LeetcodeID: p.LeetcodeFrontendID,
			Title:      p.Title,
			Difficulty: p.Difficulty,
			URL:        p.URL,
			Topics:     p.TopicTags,
			Status:     up.Status,
			Completed:  completed,
			Journal:    journal,
		})
	}

	sortSessionProblems(card.Problems)
	card.Done = card.TotalCount > 0 && card.CompletedCount == card.TotalCount
	return card, nil
}

func sortSessionProblems(problems []sessionProblem) {
	sort.SliceStable(problems, func(i, j int) bool {
		left, leftOK := strconv.Atoi(problems[i].LeetcodeID)
		right, rightOK := strconv.Atoi(problems[j].LeetcodeID)
		switch {
		case leftOK == nil && rightOK == nil:
			if left != right {
				return left < right
			}
		case leftOK == nil:
			return true
		case rightOK == nil:
			return false
		}
		return problems[i].Title < problems[j].Title
	})
}

func normalizeCompletionFilter(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "solved":
		return "solved"
	case "not-solved", "not_solved", "unsolved":
		return "not-solved"
	default:
		return ""
	}
}

func sessionPollURL(basePath string, sessionID int64, filter string) string {
	path := fmt.Sprintf("/session/%d/next", sessionID)
	if filter == "" {
		return web.AppPath(basePath, path)
	}
	q := url.Values{}
	q.Set("filter", filter)
	return web.AppPath(basePath, path) + "?" + q.Encode()
}

func (s *server) handleProblems(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	filters := store.ProblemFilters{
		State:      r.URL.Query().Get("filter"),
		Pattern:    strings.TrimSpace(r.URL.Query().Get("pattern")),
		Difficulty: normalizeDifficultyFilter(r.URL.Query().Get("difficulty")),
		Acceptance: normalizeAcceptanceFilter(r.URL.Query().Get("acceptance")),
	}
	page := parsePage(r.URL.Query().Get("page"))
	const pageSize = 100
	total, err := store.CountProblemsForUser(r.Context(), s.store.DB(), uid, filters)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	totalPageCount := totalPages(total, pageSize)
	if page > totalPageCount {
		page = totalPageCount
	}
	offset := (page - 1) * pageSize
	items, err := store.ListProblemsForUser(r.Context(), s.store.DB(), uid, filters, pageSize, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	patterns, err := store.ListPatternsWithStrength(r.Context(), s.store.DB(), uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	start := 0
	end := 0
	if len(items) > 0 {
		start = offset + 1
		end = offset + len(items)
	}
	s.page(w, "problems", web.PageData{
		Title:   "Problems",
		UserID:  uid,
		NavItem: "problems",
		Data: problemsPageData{
			Filter:            filters.State,
			Pattern:           filters.Pattern,
			Difficulty:        filters.Difficulty,
			Acceptance:        filters.Acceptance,
			AcceptanceBuckets: acceptanceBuckets(),
			Patterns:          patterns,
			Problems:          items,
			Page:              page,
			TotalPages:        totalPageCount,
			TotalCount:        total,
			Start:             start,
			End:               end,
			HasPrev:           page > 1,
			HasNext:           page < totalPageCount,
			PrevURL:           problemPageURL(s.basePath, filters, page-1),
			NextURL:           problemPageURL(s.basePath, filters, page+1),
		},
	})
}

type problemsPageData struct {
	Filter            string
	Pattern           string
	Difficulty        string
	Acceptance        string
	AcceptanceBuckets []acceptanceBucket
	Patterns          []store.PatternStrength
	Problems          []store.ProblemListItem
	Page              int
	TotalPages        int
	TotalCount        int
	Start             int
	End               int
	HasPrev           bool
	HasNext           bool
	PrevURL           string
	NextURL           string
}

type acceptanceBucket struct {
	Value string
	Label string
}

func totalPages(total, pageSize int) int {
	if pageSize <= 0 {
		pageSize = 100
	}
	if total <= 0 {
		return 1
	}
	return (total + pageSize - 1) / pageSize
}

func parsePage(raw string) int {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func problemPageURL(basePath string, filters store.ProblemFilters, page int) string {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	if filters.Acceptance != "" {
		q.Set("acceptance", filters.Acceptance)
	}
	if filters.Difficulty != "" {
		q.Set("difficulty", filters.Difficulty)
	}
	if filters.State != "" {
		q.Set("filter", filters.State)
	}
	if filters.Pattern != "" {
		q.Set("pattern", filters.Pattern)
	}
	q.Set("page", strconv.Itoa(page))
	return web.AppPath(basePath, "/problems") + "?" + q.Encode()
}

func normalizeDifficultyFilter(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "easy":
		return string(models.DifficultyEasy)
	case "medium", "mid":
		return string(models.DifficultyMedium)
	case "hard", "high":
		return string(models.DifficultyHard)
	default:
		return ""
	}
}

func normalizeAcceptanceFilter(raw string) string {
	bound, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || bound < 0 || bound > 90 || bound%10 != 0 {
		return ""
	}
	return strconv.Itoa(bound)
}

func acceptanceBuckets() []acceptanceBucket {
	buckets := make([]acceptanceBucket, 0, 10)
	for lower := 0; lower <= 90; lower += 10 {
		upper := lower + 9
		if lower == 90 {
			upper = 100
		}
		buckets = append(buckets, acceptanceBucket{
			Value: strconv.Itoa(lower),
			Label: fmt.Sprintf("%d-%d%%", lower, upper),
		})
	}
	return buckets
}

func (s *server) handleProblemDetail(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	detail, err := store.GetProblemDetail(r.Context(), s.store.DB(), uid, chi.URLParam(r, "slug"), 25)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.page(w, "problem_detail", web.PageData{
		Title:   detail.Problem.Title,
		UserID:  uid,
		NavItem: "problems",
		Data:    detail,
	})
}

func (s *server) handleProblemJournal(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	problemID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || problemID <= 0 {
		http.Error(w, "bad problem id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	err = store.UpdateLatestAttemptJournal(r.Context(), s.store.DB(), uid, problemID, r.FormValue("journal"))
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "no attempt to annotate yet", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("saved"))
}

func (s *server) handleProblemTriage(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	problemID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || problemID <= 0 {
		http.Error(w, "bad problem id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	action := r.FormValue("action")
	if err := store.TriageUserProblem(r.Context(), s.store.DB(), uid, problemID, action); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ref := r.Header.Get("Referer")
	if ref == "" {
		ref = s.appPath("/problems")
	}
	http.Redirect(w, r, ref, http.StatusSeeOther)
}

func (s *server) handlePatterns(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	patterns, err := store.ListPatternsWithStrength(r.Context(), s.store.DB(), uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.page(w, "patterns", web.PageData{
		Title:   "Patterns",
		UserID:  uid,
		NavItem: "patterns",
		Data:    patternsPageData{Patterns: patterns},
	})
}

type patternsPageData struct {
	Patterns []store.PatternStrength
}

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	recent, err := store.RecentAttempts(r.Context(), s.store.DB(), uid, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.page(w, "stats", web.PageData{
		Title:   "Stats",
		UserID:  uid,
		NavItem: "stats",
		Data:    statsPageData{RecentAttempts: recent},
	})
}

type statsPageData struct {
	RecentAttempts []store.RecentAttempt
}

func (s *server) handleSettings(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	c, err := store.GetLeetcodeCookies(r.Context(), s.store.DB(), uid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	vacation, err := store.GetVacationUntil(r.Context(), s.store.DB(), uid)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	status := "no cookies stored"
	if len(c.SessionEnc) > 0 {
		if c.Valid {
			status = "cookies stored and valid"
		} else {
			status = "cookies stored but marked invalid; re-sync via extension"
		}
	}
	s.page(w, "settings", web.PageData{
		Title:   "Settings",
		UserID:  uid,
		NavItem: "settings",
		Data: settingsPageData{
			Username:      emptyDash(c.Username),
			CookieStatus:  status,
			VacationUntil: vacation,
			Message:       r.URL.Query().Get("message"),
		},
	})
}

type settingsPageData struct {
	Username        string
	CookieStatus    string
	CookieUpdatedAt *time.Time
	VacationUntil   *time.Time
	Message         string
}

func (s *server) handleSettingsColdStart(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	result, err := s.runColdStart(r.Context(), uid, r.FormValue("username"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	msg := fmt.Sprintf("imported recent=%d authed=%d duplicates=%d unknown=%d",
		result.RecentImported, result.AuthedImported, result.DuplicatesSkipped, result.UnknownSkipped)
	http.Redirect(w, r, s.appPath("/settings")+"?message="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *server) handleSettingsVacation(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var until *time.Time
	if r.FormValue("action") == "start" {
		days, _ := strconv.Atoi(r.FormValue("days"))
		if days <= 0 {
			days = 7
		}
		t := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
		until = &t
	}
	if err := store.SetVacationUntil(r.Context(), s.store.DB(), uid, until); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	msg := "vacation mode disabled"
	if until != nil {
		msg = fmt.Sprintf("vacation mode enabled until %s", until.Format("2006-01-02"))
	}
	http.Redirect(w, r, s.appPath("/settings")+"?message="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *server) runColdStart(ctx context.Context, userID int64, username string) (ldsync.ColdStartResult, error) {
	importer := &ldsync.ColdStartImporter{
		Store:              s.store,
		Vault:              s.vault,
		Client:             leetcode.New(),
		MaxSubmissionPages: 20,
		SubmissionPageSize: 50,
	}
	return importer.Run(ctx, userID, username)
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

// ---- Extension API ----

type handshakeReq struct {
	Email           string `json:"email"`
	Password        string `json:"password"`
	WebSessionToken string `json:"web_session_token"`
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
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
	}

	webToken := strings.TrimSpace(req.WebSessionToken)
	if webToken == "" {
		if cookie, err := r.Cookie(auth.CookieName); err == nil {
			webToken = strings.TrimSpace(cookie.Value)
		}
	}
	if webToken != "" {
		hash, err := auth.HashToken(webToken)
		if err != nil {
			http.Error(w, "bad web session", http.StatusUnauthorized)
			return
		}
		userID, err := store.LookupAuthSession(r.Context(), s.store.DB(), store.AuthKindWeb, hash)
		if err != nil {
			http.Error(w, "bad web session", http.StatusUnauthorized)
			return
		}
		tok, err := s.authmw.IssueExtToken(r.Context(), userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, handshakeResp{Token: tok, ExpiresIn: int(auth.ExtTokenDuration.Seconds())})
		return
	}

	if req.Email == "" || req.Password == "" {
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
	Slug            string `json:"slug"`
	Verdict         string `json:"verdict"`          // human form: "Accepted", "Wrong Answer", ...
	SubmissionCount int    `json:"submission_count"` // submissions in this session up to and including this one
	TimeTakenSec    int    `json:"time_taken_sec"`   // wall-clock since page-open
	RuntimeMs       *int   `json:"runtime_ms,omitempty"`
	MemoryKB        *int   `json:"memory_kb,omitempty"`
	Language        string `json:"language,omitempty"`
	Code            string `json:"code,omitempty"`
	LeetcodeSubmID  string `json:"leetcode_submission_id,omitempty"`
	StartedAtUnix   int64  `json:"started_at_unix,omitempty"`
	CompletedAtUnix int64  `json:"completed_at_unix,omitempty"`
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
	Slug       string            `json:"slug"`
	URL        string            `json:"url"`
	Title      string            `json:"title"`
	Difficulty models.Difficulty `json:"difficulty"`
	Reason     string            `json:"reason"`
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

type coldStartReq struct {
	Username string `json:"username"`
}

func (s *server) handleExtColdStart(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxReqBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req coldStartReq
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "expected {username}", http.StatusBadRequest)
			return
		}
	}
	result, err := s.runColdStart(r.Context(), auth.UserID(r.Context()), req.Username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, result)
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
