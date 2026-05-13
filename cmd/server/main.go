// Command server is the leetdrill web server entry point.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"leetdrill/internal/auth"
	"leetdrill/internal/leetcode"
	"leetdrill/internal/mailer"
	"leetdrill/internal/models"
	"leetdrill/internal/store"
	ldsync "leetdrill/internal/sync"
	"leetdrill/internal/vault"
	"leetdrill/internal/web"
)

const maxReqBody = 256 * 1024 // 256 KB; submissions can include code

type ipRateLimiter struct {
	mu      sync.Mutex
	entries map[string][]time.Time
	limit   int
	window  time.Duration
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		entries: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
	}
}

func (l *ipRateLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	times := l.entries[ip]
	j := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[j] = t
			j++
		}
	}
	times = times[:j]
	if len(times) >= l.limit {
		l.entries[ip] = times
		return false
	}
	l.entries[ip] = append(times, now)
	return true
}

type server struct {
	addr          string
	store         *store.Store
	vault         *vault.Vault
	authmw        *auth.Authenticator
	renderer      *web.Renderer
	basePath      string
	mailer        *mailer.Mailer
	resendLimiter *ipRateLimiter
	googleOAuth   *auth.GoogleOAuth
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

	appBase := os.Getenv("LEETDRILL_APP_BASE")
	if appBase == "" {
		appBase = "http://localhost" + addr
	}

	googleOAuth, err := auth.GoogleOAuthFromEnv(appBase)
	if err != nil {
		log.Printf("warning: google oauth not configured: %v", err)
	}

	var ml *mailer.Mailer
	if !strings.EqualFold(os.Getenv("SINGLE_USER"), "true") {
		ml, err = mailer.FromEnv(appBase)
		if err != nil {
			log.Printf("warning: mailer not configured: %v", err)
		}
	}

	srv := &server{
		addr:          addr,
		store:         st,
		vault:         v,
		authmw:        authmw,
		renderer:      renderer,
		basePath:      basePath,
		mailer:        ml,
		resendLimiter: newIPRateLimiter(5, time.Hour),
		googleOAuth:   googleOAuth,
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
	r.Get("/favicon.svg", handleFavicon)

	r.Get("/login", s.handleLoginPage)
	r.Post("/login", s.handleLoginSubmit)
	r.Get("/signup", s.handleSignupPage)
	r.Post("/signup", s.handleSignupSubmit)
	r.Get("/auth/google/start", s.handleGoogleStart)
	r.Get("/auth/google/callback", s.handleGoogleCallback)
	r.Post("/logout", s.handleLogout)
	r.Get("/extension/connect", s.handleExtensionConnect)
	r.Get("/verify-pending", s.handleVerifyPending)
	r.Get("/verify", s.handleVerifyEmail)
	r.Post("/resend-verify", s.handleResendVerify)
	r.Get("/forgot", s.handleForgotPage)
	r.Post("/forgot", s.handleForgotSubmit)
	r.Get("/reset", s.handleResetPage)
	r.Post("/reset", s.handleResetSubmit)

	r.Group(func(r chi.Router) {
		r.Use(s.authmw.RequireWebSession)
		r.Get("/", s.handleHome)
		r.Post("/session/start", s.handleSessionStart)
		r.Get("/session/today", s.handleSessionToday)
		r.Get("/session/{id}/next", s.handleSessionNext)
		r.Get("/lists", s.handleLists)
		r.Get("/lists/{slug}", s.handleListDetail)
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
			r.Get("/today-problems", s.handleExtTodayProblems)
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

func handleFavicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(ldLogoSVG))
}

// ---- HTML ----

const ldLogoSVG = `<svg aria-label="LeetDrill logo" role="img" viewBox="0 0 64 64" class="h-8 w-8 shrink-0 rounded-md">
          <rect width="64" height="64" rx="12" fill="#18181b"></rect>
          <text x="32" y="39" text-anchor="middle" font-family="Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="24" font-weight="800" fill="#f4f4f5">LD</text>
        </svg>`

const authBrand = `<div class="flex items-center gap-2 text-sm font-semibold uppercase tracking-normal text-zinc-500">` + ldLogoSVG + `<span>LeetDrill</span></div>`

const themeHead = `<script>
      (function () {
        var key = "leetdrill-theme";
        var modes = ["system", "dark", "light"];
        function clean(mode) {
          return modes.indexOf(mode) === -1 ? "system" : mode;
        }
        function wantsDark(mode) {
          return mode === "dark" || (mode === "system" && window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches);
        }
        function apply(mode) {
          mode = clean(mode);
          document.documentElement.classList.toggle("dark", wantsDark(mode));
          document.documentElement.dataset.theme = mode;
          var label = mode.charAt(0).toUpperCase() + mode.slice(1);
          document.querySelectorAll("[data-theme-label]").forEach(function (el) { el.textContent = label; });
          document.querySelectorAll("[data-theme-toggle]").forEach(function (el) { el.setAttribute("aria-label", "Theme: " + label); });
        }
        window.leetdrillTheme = {
          apply: apply,
          next: function () {
            var current = clean(localStorage.getItem(key) || "system");
            var next = modes[(modes.indexOf(current) + 1) %% modes.length];
            localStorage.setItem(key, next);
            apply(next);
          }
        };
        try { apply(localStorage.getItem(key) || "system"); } catch (_) { apply("system"); }
        document.addEventListener("DOMContentLoaded", function () {
          document.querySelectorAll("[data-theme-toggle]").forEach(function (el) {
            el.addEventListener("click", window.leetdrillTheme.next);
          });
          apply(clean(localStorage.getItem(key) || "system"));
        });
        if (window.matchMedia) {
          window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function () {
            apply(clean(localStorage.getItem(key) || "system"));
          });
        }
      })();
    </script>
    <script src="https://cdn.tailwindcss.com"></script>
    <style>
      :root { color-scheme: light; }
      .dark { color-scheme: dark; }
      .dark body, .dark .bg-zinc-50 { background-color: #09090b !important; color: #f4f4f5 !important; }
      .dark .bg-white { background-color: #18181b !important; }
      .dark .bg-zinc-100 { background-color: #27272a !important; }
      .dark .bg-zinc-900 { background-color: #f4f4f5 !important; color: #09090b !important; }
      .dark svg[aria-label="LeetDrill logo"] rect { fill: #f4f4f5 !important; }
      .dark svg[aria-label="LeetDrill logo"] text { fill: #18181b !important; }
      .dark .hover\:bg-zinc-50:hover, .dark .hover\:bg-zinc-100:hover { background-color: #27272a !important; }
      .dark .hover\:bg-zinc-800:hover { background-color: #e4e4e7 !important; }
      .dark .text-zinc-950, .dark .text-zinc-900, .dark .text-zinc-800, .dark .text-zinc-700 { color: #e4e4e7 !important; }
      .dark .text-zinc-600, .dark .text-zinc-500 { color: #a1a1aa !important; }
      .dark .text-zinc-400 { color: #71717a !important; }
      .dark .border-zinc-100, .dark .border-zinc-200, .dark .border-zinc-300 { border-color: #3f3f46 !important; }
      .dark .divide-zinc-100 > :not([hidden]) ~ :not([hidden]), .dark .divide-zinc-200 > :not([hidden]) ~ :not([hidden]) { border-color: #27272a !important; }
      .dark .bg-sky-50 { background-color: #082f49 !important; }
      .dark .border-sky-200 { border-color: #0369a1 !important; }
      .dark .text-sky-900 { color: #bae6fd !important; }
      .dark .bg-emerald-50 { background-color: #022c22 !important; }
      .dark .border-emerald-200 { border-color: #047857 !important; }
      .dark .text-emerald-950, .dark .text-emerald-900, .dark .text-emerald-800 { color: #a7f3d0 !important; }
      .dark .rounded-full.bg-emerald-100.text-emerald-800 { color: #065f46 !important; }
      .dark .bg-rose-50 { background-color: #4c0519 !important; }
      .dark .border-rose-200 { border-color: #be123c !important; }
      .dark .text-rose-600 { color: #fda4af !important; }
      .dark input, .dark select, .dark textarea { background-color: #18181b !important; border-color: #3f3f46 !important; color: #f4f4f5 !important; }
      .dark input::placeholder, .dark textarea::placeholder { color: #71717a !important; }
      .dark .shadow-sm { box-shadow: 0 1px 2px 0 rgb(0 0 0 / 0.35) !important; }
    </style>`

const authThemeToggle = `<button type="button" data-theme-toggle class="fixed right-4 top-4 z-10 rounded-md border border-zinc-200 bg-white px-3 py-1.5 text-xs font-medium text-zinc-700 shadow-sm hover:bg-zinc-50">
      Theme: <span data-theme-label>System</span>
    </button>`

const loginPage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Login · LeetDrill</title>
    <link rel="icon" type="image/svg+xml" href="favicon.svg">
    ` + themeHead + `
  </head>
  <body class="min-h-screen bg-zinc-50 text-zinc-950">
    ` + authThemeToggle + `
    <main class="mx-auto grid min-h-screen max-w-6xl items-center gap-10 px-4 py-10 sm:px-6 lg:grid-cols-[1fr_420px] lg:px-8">
      <section class="max-w-xl">
        ` + authBrand + `
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
        <a class="mt-6 flex w-full items-center justify-center rounded-md border border-zinc-300 bg-white px-4 py-2.5 text-sm font-medium text-zinc-800 hover:bg-zinc-50 focus:outline-none focus:ring-2 focus:ring-zinc-900 focus:ring-offset-2" href="%s">Continue with Google</a>
        <div class="mt-5 flex items-center gap-3 text-xs uppercase tracking-normal text-zinc-400">
          <div class="h-px flex-1 bg-zinc-200"></div>
          <span>Email</span>
          <div class="h-px flex-1 bg-zinc-200"></div>
        </div>
        <form class="mt-6 space-y-4" method="post" action="%s">
          <input type="hidden" name="next" value="%s">
          <div>
            <label class="block text-sm font-medium text-zinc-700" for="email">Email</label>
            <input id="email" class="mt-2 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-2 focus:ring-zinc-900/10" type="email" name="email" autocomplete="email" autofocus required>
          </div>
          <div>
            <div class="flex items-center justify-between">
              <label class="block text-sm font-medium text-zinc-700" for="password">Password</label>
              <a class="text-xs text-zinc-500 underline hover:text-zinc-800" href="%s">Forgot password?</a>
            </div>
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
    <link rel="icon" type="image/svg+xml" href="favicon.svg">
    ` + themeHead + `
  </head>
  <body class="min-h-screen bg-zinc-50 text-zinc-950">
    ` + authThemeToggle + `
    <main class="mx-auto grid min-h-screen max-w-6xl items-center gap-10 px-4 py-10 sm:px-6 lg:grid-cols-[1fr_420px] lg:px-8">
      <section class="max-w-xl">
        ` + authBrand + `
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
        <a class="mt-6 flex w-full items-center justify-center rounded-md border border-zinc-300 bg-white px-4 py-2.5 text-sm font-medium text-zinc-800 hover:bg-zinc-50 focus:outline-none focus:ring-2 focus:ring-zinc-900 focus:ring-offset-2" href="%s">Continue with Google</a>
        <div class="mt-5 flex items-center gap-3 text-xs uppercase tracking-normal text-zinc-400">
          <div class="h-px flex-1 bg-zinc-200"></div>
          <span>Email</span>
          <div class="h-px flex-1 bg-zinc-200"></div>
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

const extensionConnectPage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta name="leetdrill-extension-token" content="%s">
    <title>Extension connected · LeetDrill</title>
    <link rel="icon" type="image/svg+xml" href="favicon.svg">
    ` + themeHead + `
  </head>
  <body class="min-h-screen bg-zinc-50 text-zinc-950">
    ` + authThemeToggle + `
    <main class="mx-auto flex min-h-screen max-w-lg items-center px-4 py-10">
      <section class="w-full rounded-lg border border-zinc-200 bg-white p-6 shadow-sm">
        ` + authBrand + `
        <h1 class="mt-3 text-2xl font-semibold tracking-normal">LeetDrill extension connected</h1>
        <p id="leetdrill-extension-status" class="mt-3 text-sm leading-6 text-zinc-600">Saving the extension token in your browser.</p>
        <div class="mt-5 rounded-lg border border-zinc-200 bg-zinc-50 p-3">
          <label class="block text-xs font-medium uppercase tracking-normal text-zinc-500" for="manual_token">Manual code</label>
          <textarea id="manual_token" class="mt-2 h-24 w-full resize-none rounded-md border border-zinc-300 bg-white p-2 font-mono text-xs text-zinc-800" readonly>%s</textarea>
          <p class="mt-2 text-xs leading-5 text-zinc-500">If the extension does not connect automatically, paste this code into the extension options page.</p>
        </div>
      </section>
    </main>
    <script>
      (function () {
        var token = document.querySelector('meta[name="leetdrill-extension-token"]').content;
        var status = document.getElementById("leetdrill-extension-status");
        var attempts = 0;
        var done = false;

        window.addEventListener("message", function (event) {
          if (event.source !== window || event.origin !== window.location.origin) return;
          if (!event.data || event.data.type !== "LEETDRILL_WEB_CONNECT_DONE") return;
          done = true;
          if (status) status.textContent = event.data.error || "Extension connected. You can close this tab.";
        });

        function announce() {
          if (done) return;
          attempts += 1;
          window.postMessage({ type: "LEETDRILL_WEB_CONNECT_TOKEN", token: token }, window.location.origin);
          if (attempts === 6 && status) {
            status.textContent = "Still waiting for the extension. Check that Firefox or Zen is running LeetDrill Companion 0.1.6 or newer, or use the manual code below.";
          }
          if (attempts < 60) window.setTimeout(announce, 500);
        }
        announce();
      })();
    </script>
  </body>
</html>`

// verifyPendingPage args: email (display), msg, resend action URL, email (hidden), login URL.
const verifyPendingPage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Verify your email · LeetDrill</title>
    <link rel="icon" type="image/svg+xml" href="favicon.svg">
    ` + themeHead + `
  </head>
  <body class="min-h-screen bg-zinc-50 text-zinc-950">
    ` + authThemeToggle + `
    <main class="mx-auto flex min-h-screen max-w-lg items-center px-4 py-10">
      <section class="w-full rounded-lg border border-zinc-200 bg-white p-6 shadow-sm">
        ` + authBrand + `
        <h1 class="mt-3 text-2xl font-semibold tracking-normal">Check your email</h1>
        <p class="mt-3 text-sm leading-6 text-zinc-600">We sent a verification link to <strong>%s</strong>. Click it to activate your account.</p>
        <p class="mt-2 text-sm text-zinc-500" aria-live="polite">%s</p>
        <form class="mt-5" method="post" action="%s">
          <input type="hidden" name="email" value="%s">
          <button class="w-full rounded-md bg-zinc-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-zinc-800 focus:outline-none focus:ring-2 focus:ring-zinc-900 focus:ring-offset-2" type="submit">Resend verification email</button>
        </form>
        <p class="mt-4 text-center text-sm text-zinc-600">Wrong account? <a class="font-medium text-zinc-950 underline" href="%s">Log in with a different account</a></p>
      </section>
    </main>
  </body>
</html>`

// forgotPage args: msg, form action URL, login URL.
const forgotPage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Forgot password · LeetDrill</title>
    <link rel="icon" type="image/svg+xml" href="favicon.svg">
    ` + themeHead + `
  </head>
  <body class="min-h-screen bg-zinc-50 text-zinc-950">
    ` + authThemeToggle + `
    <main class="mx-auto flex min-h-screen max-w-lg items-center px-4 py-10">
      <section class="w-full rounded-lg border border-zinc-200 bg-white p-6 shadow-sm">
        ` + authBrand + `
        <h1 class="mt-3 text-2xl font-semibold tracking-normal">Reset your password</h1>
        <p class="mt-2 text-sm text-zinc-500" aria-live="polite">%s</p>
        <form class="mt-5 space-y-4" method="post" action="%s">
          <div>
            <label class="block text-sm font-medium text-zinc-700" for="email">Email</label>
            <input id="email" class="mt-2 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-2 focus:ring-zinc-900/10" type="email" name="email" autocomplete="email" autofocus required>
          </div>
          <button class="w-full rounded-md bg-zinc-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-zinc-800 focus:outline-none focus:ring-2 focus:ring-zinc-900 focus:ring-offset-2" type="submit">Send reset link</button>
        </form>
        <p class="mt-4 text-center text-sm text-zinc-600"><a class="font-medium text-zinc-950 underline" href="%s">Back to login</a></p>
      </section>
    </main>
  </body>
</html>`

// resetPage args: msg, form action URL, token (hidden field value).
const resetPage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Set new password · LeetDrill</title>
    <link rel="icon" type="image/svg+xml" href="favicon.svg">
    ` + themeHead + `
  </head>
  <body class="min-h-screen bg-zinc-50 text-zinc-950">
    ` + authThemeToggle + `
    <main class="mx-auto flex min-h-screen max-w-lg items-center px-4 py-10">
      <section class="w-full rounded-lg border border-zinc-200 bg-white p-6 shadow-sm">
        ` + authBrand + `
        <h1 class="mt-3 text-2xl font-semibold tracking-normal">Set new password</h1>
        <p class="mt-2 text-sm text-zinc-500" aria-live="polite">%s</p>
        <form class="mt-5 space-y-4" method="post" action="%s">
          <input type="hidden" name="token" value="%s">
          <div>
            <label class="block text-sm font-medium text-zinc-700" for="password">New password</label>
            <input id="password" class="mt-2 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-2 focus:ring-zinc-900/10" type="password" name="password" autocomplete="new-password" minlength="8" autofocus required>
          </div>
          <div>
            <label class="block text-sm font-medium text-zinc-700" for="confirm_password">Confirm password</label>
            <input id="confirm_password" class="mt-2 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-2 focus:ring-zinc-900/10" type="password" name="confirm_password" autocomplete="new-password" minlength="8" required>
          </div>
          <button class="w-full rounded-md bg-zinc-900 px-4 py-2.5 text-sm font-medium text-white hover:bg-zinc-800 focus:outline-none focus:ring-2 focus:ring-zinc-900 focus:ring-offset-2" type="submit">Set password</button>
        </form>
      </section>
    </main>
  </body>
</html>`

// verifyDonePage args: title (head), heading, message, link URL, link text.
const verifyDonePage = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>%s · LeetDrill</title>
    <link rel="icon" type="image/svg+xml" href="favicon.svg">
    ` + themeHead + `
  </head>
  <body class="min-h-screen bg-zinc-50 text-zinc-950">
    ` + authThemeToggle + `
    <main class="mx-auto flex min-h-screen max-w-lg items-center px-4 py-10">
      <section class="w-full rounded-lg border border-zinc-200 bg-white p-6 shadow-sm">
        ` + authBrand + `
        <h1 class="mt-3 text-2xl font-semibold tracking-normal">%s</h1>
        <p class="mt-3 text-sm leading-6 text-zinc-600">%s</p>
        <a class="mt-5 block w-full rounded-md bg-zinc-900 px-4 py-2.5 text-center text-sm font-medium text-white hover:bg-zinc-800" href="%s">%s</a>
      </section>
    </main>
  </body>
</html>`

func renderExtensionConnectPage(token string) string {
	escaped := html.EscapeString(token)
	return fmt.Sprintf(extensionConnectPage, escaped, escaped)
}

func (s *server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	next := ""
	if raw := strings.TrimSpace(r.URL.Query().Get("next")); raw != "" {
		next = s.safeLoginNext(raw)
	}
	msg := "sign in to continue."
	if r.URL.Query().Get("reset") == "1" {
		msg = "Password updated. Log in with your new password."
	}
	_, _ = fmt.Fprintf(w, loginPage, html.EscapeString(msg), s.googleStartURL(next), s.appPath("/login"), html.EscapeString(next), s.appPath("/forgot"), s.appPath("/signup"))
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
		next := s.safeLoginNext(r.FormValue("next"))
		_, _ = fmt.Fprintf(w, loginPage, "invalid email or password.", s.googleStartURL(next), s.appPath("/login"), html.EscapeString(next), s.appPath("/forgot"), s.appPath("/signup"))
		return
	}
	if err := auth.VerifyPassword(hash, pw); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		next := s.safeLoginNext(r.FormValue("next"))
		_, _ = fmt.Fprintf(w, loginPage, "invalid email or password.", s.googleStartURL(next), s.appPath("/login"), html.EscapeString(next), s.appPath("/forgot"), s.appPath("/signup"))
		return
	}
	token, err := s.authmw.IssueWebToken(r.Context(), userID)
	if err != nil {
		http.Error(w, "issue token", http.StatusInternalServerError)
		return
	}
	s.authmw.SetSessionCookie(w, token)
	http.Redirect(w, r, s.safeLoginNext(r.FormValue("next")), http.StatusSeeOther)
}

func (s *server) safeLoginNext(raw string) string {
	raw = strings.TrimSpace(raw)
	fallback := s.appPath("/")
	if raw == "" || strings.HasPrefix(raw, "//") {
		return fallback
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Path == "" {
		return fallback
	}
	if !strings.HasPrefix(u.Path, s.appPath("/")) {
		return fallback
	}
	return u.RequestURI()
}

func (s *server) handleSignupPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, signupPage, "create an account.", s.googleStartURL(""), s.appPath("/signup"), s.appPath("/login"))
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
		_, _ = fmt.Fprintf(w, signupPage, message, s.googleStartURL(""), s.appPath("/signup"), s.appPath("/login"))
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
		_, _ = fmt.Fprintf(w, signupPage, "could not create account.", s.googleStartURL(""), s.appPath("/signup"), s.appPath("/login"))
		return
	}
	vtok, vhash, err := auth.NewToken()
	if err != nil {
		log.Printf("signup: new token: %v", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, signupPage, "an error occurred. please try again.", s.googleStartURL(""), s.appPath("/signup"), s.appPath("/login"))
		return
	}
	if err := store.CreateEmailToken(r.Context(), s.store.DB(), userID, store.EmailTokenVerify,
		vhash, time.Now().Add(6*time.Hour)); err != nil {
		log.Printf("signup: create email token: %v", err)
	}
	if s.mailer != nil {
		if err := s.mailer.SendVerify(email, vtok); err != nil {
			log.Printf("signup: send verify email to %s: %v", email, err)
		}
	}
	q2 := url.Values{"email": {email}}
	http.Redirect(w, r, s.appPath("/verify-pending")+"?"+q2.Encode(), http.StatusSeeOther)
}

const googleOAuthStateCookie = "ld_google_oauth_state"

func (s *server) googleStartURL(next string) string {
	u := s.appPath("/auth/google/start")
	next = strings.TrimSpace(next)
	if next == "" {
		return u
	}
	q := url.Values{"next": {next}}
	return u + "?" + q.Encode()
}

func (s *server) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	if s.googleOAuth == nil {
		http.Error(w, "google login not configured", http.StatusNotFound)
		return
	}
	next := s.safeLoginNext(r.URL.Query().Get("next"))
	state, cookieValue, err := newGoogleOAuthState(next)
	if err != nil {
		http.Error(w, "google state", http.StatusInternalServerError)
		return
	}
	s.setGoogleOAuthStateCookie(w, r, cookieValue)
	http.Redirect(w, r, s.googleOAuth.AuthCodeURL(state), http.StatusSeeOther)
}

func (s *server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if s.googleOAuth == nil {
		http.Error(w, "google login not configured", http.StatusNotFound)
		return
	}
	if r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, s.appPath("/login"), http.StatusSeeOther)
		return
	}
	cookie, err := r.Cookie(googleOAuthStateCookie)
	if err != nil {
		http.Error(w, "missing google state", http.StatusBadRequest)
		return
	}
	s.clearGoogleOAuthStateCookie(w, r)
	next, ok := parseGoogleOAuthState(r.URL.Query().Get("state"), cookie.Value)
	if !ok {
		http.Error(w, "invalid google state", http.StatusBadRequest)
		return
	}
	user, err := s.googleOAuth.ExchangeUser(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		log.Printf("google login: exchange: %v", err)
		http.Redirect(w, r, s.appPath("/login"), http.StatusSeeOther)
		return
	}
	userID, err := store.EnsureGoogleUser(r.Context(), s.store.DB(), user.Sub, user.Email)
	if err != nil {
		log.Printf("google login: ensure user: %v", err)
		http.Redirect(w, r, s.appPath("/login"), http.StatusSeeOther)
		return
	}
	token, err := s.authmw.IssueWebToken(r.Context(), userID)
	if err != nil {
		http.Error(w, "issue token", http.StatusInternalServerError)
		return
	}
	s.authmw.SetSessionCookie(w, token)
	http.Redirect(w, r, s.safeLoginNext(next), http.StatusSeeOther)
}

func newGoogleOAuthState(next string) (state, cookieValue string, err error) {
	state, _, err = auth.NewToken()
	if err != nil {
		return "", "", err
	}
	next = base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(next)))
	return state, state + "." + next, nil
}

func parseGoogleOAuthState(state, cookieValue string) (string, bool) {
	state = strings.TrimSpace(state)
	parts := strings.SplitN(cookieValue, ".", 2)
	if state == "" || len(parts) != 2 {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(parts[0])) != 1 {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func (s *server) setGoogleOAuthStateCookie(w http.ResponseWriter, _ *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     googleOAuthStateCookie,
		Value:    value,
		Path:     s.cookiePath(),
		Expires:  time.Now().Add(10 * time.Minute),
		MaxAge:   600,
		HttpOnly: true,
		Secure:   s.authmw != nil && s.authmw.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) clearGoogleOAuthStateCookie(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     googleOAuthStateCookie,
		Value:    "",
		Path:     s.cookiePath(),
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.authmw != nil && s.authmw.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) cookiePath() string {
	base := strings.TrimRight(strings.TrimSpace(s.basePath), "/")
	if base == "" || base == "/" {
		return "/"
	}
	if !strings.HasPrefix(base, "/") {
		return "/" + base
	}
	return base
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

func (s *server) handleVerifyPending(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	email := r.URL.Query().Get("email")
	msg := ""
	if r.URL.Query().Get("resent") == "1" {
		msg = "Verification email resent."
	}
	_, _ = fmt.Fprintf(w, verifyPendingPage,
		html.EscapeString(email),
		html.EscapeString(msg),
		s.appPath("/resend-verify"),
		html.EscapeString(email),
		s.appPath("/login"),
	)
}

func (s *server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tok := r.URL.Query().Get("token")
	if tok == "" {
		_, _ = fmt.Fprintf(w, verifyDonePage,
			"Invalid link", "Invalid verification link",
			"The link is missing or malformed.",
			s.appPath("/verify-pending"), "Try again",
		)
		return
	}
	hash, err := auth.HashToken(tok)
	if err != nil {
		_, _ = fmt.Fprintf(w, verifyDonePage,
			"Invalid link", "Invalid verification link",
			"The link is malformed.",
			s.appPath("/verify-pending"), "Try again",
		)
		return
	}
	userID, err := store.ConsumeEmailToken(r.Context(), s.store.DB(), hash, store.EmailTokenVerify)
	if err != nil {
		_, _ = fmt.Fprintf(w, verifyDonePage,
			"Link expired", "Verification link expired or already used",
			"Request a new link below.",
			s.appPath("/verify-pending"), "Resend verification email",
		)
		return
	}
	if err := store.MarkEmailVerified(r.Context(), s.store.DB(), userID); err != nil {
		log.Printf("verify email: mark verified user %d: %v", userID, err)
	}
	_, _ = fmt.Fprintf(w, verifyDonePage,
		"Email verified", "Email verified",
		"Your email is confirmed. You can now log in.",
		s.appPath("/login"), "Log in",
	)
}

func (s *server) handleResendVerify(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.resendLimiter.Allow(ip) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	userID, verifiedAt, err := store.GetUserIDByEmail(r.Context(), s.store.DB(), email)
	if err == nil && verifiedAt == nil {
		tok, hash, err := auth.NewToken()
		if err == nil {
			_ = store.CreateEmailToken(r.Context(), s.store.DB(), userID, store.EmailTokenVerify,
				hash, time.Now().Add(6*time.Hour))
			if s.mailer != nil {
				if err := s.mailer.SendVerify(email, tok); err != nil {
					log.Printf("resend verify: %v", err)
				}
			}
		}
	}
	q := url.Values{"email": {email}, "resent": {"1"}}
	http.Redirect(w, r, s.appPath("/verify-pending")+"?"+q.Encode(), http.StatusSeeOther)
}

func (s *server) handleForgotPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msg := ""
	if r.URL.Query().Get("sent") == "1" {
		msg = "If an account with that email exists, a reset link has been sent."
	}
	_, _ = fmt.Fprintf(w, forgotPage, html.EscapeString(msg), s.appPath("/forgot"), s.appPath("/login"))
}

func (s *server) handleForgotSubmit(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	userID, _, err := store.GetUserIDByEmail(r.Context(), s.store.DB(), email)
	if err == nil {
		tok, hash, err := auth.NewToken()
		if err == nil {
			_ = store.CreateEmailToken(r.Context(), s.store.DB(), userID, store.EmailTokenReset,
				hash, time.Now().Add(6*time.Hour))
			if s.mailer != nil {
				if err := s.mailer.SendReset(email, tok); err != nil {
					log.Printf("forgot: send reset to %s: %v", email, err)
				}
			}
		}
	}
	http.Redirect(w, r, s.appPath("/forgot")+"?sent=1", http.StatusSeeOther)
}

func (s *server) handleResetPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tok := r.URL.Query().Get("token")
	if tok == "" {
		http.Redirect(w, r, s.appPath("/forgot"), http.StatusSeeOther)
		return
	}
	_, _ = fmt.Fprintf(w, resetPage, "", s.appPath("/reset"), html.EscapeString(tok))
}

func (s *server) handleResetSubmit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tok := r.FormValue("token")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")

	showErr := func(msg string) {
		_, _ = fmt.Fprintf(w, resetPage, html.EscapeString(msg), s.appPath("/reset"), html.EscapeString(tok))
	}

	if len(password) < 8 {
		showErr("Password must be at least 8 characters.")
		return
	}
	if password != confirm {
		showErr("Passwords do not match.")
		return
	}
	hash, err := auth.HashToken(tok)
	if err != nil {
		showErr("Invalid reset link.")
		return
	}
	userID, err := store.ConsumeEmailToken(r.Context(), s.store.DB(), hash, store.EmailTokenReset)
	if err != nil {
		showErr("Reset link has expired or already been used. Request a new one.")
		return
	}
	pwHash, err := auth.HashPassword(password)
	if err != nil {
		log.Printf("reset: hash password: %v", err)
		showErr("An error occurred. Please try again.")
		return
	}
	if err := store.SetPasswordHash(r.Context(), s.store.DB(), userID, pwHash); err != nil {
		log.Printf("reset: set password: %v", err)
		showErr("An error occurred. Please try again.")
		return
	}
	http.Redirect(w, r, s.appPath("/login")+"?reset=1", http.StatusSeeOther)
}

func (s *server) handleExtensionConnect(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.extensionConnectUserID(r)
	if !ok {
		loginURL := s.appPath("/login") + "?next=" + url.QueryEscape(s.appPath("/extension/connect"))
		http.Redirect(w, r, loginURL, http.StatusSeeOther)
		return
	}
	token, err := s.authmw.IssueExtToken(r.Context(), userID)
	if err != nil {
		http.Error(w, "issue extension token", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, renderExtensionConnectPage(token))
}

func (s *server) extensionConnectUserID(r *http.Request) (int64, bool) {
	if s.authmw.SingleUserID != 0 {
		return s.authmw.SingleUserID, true
	}
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return 0, false
	}
	hash, err := auth.HashToken(cookie.Value)
	if err != nil {
		return 0, false
	}
	userID, err := store.LookupAuthSession(r.Context(), s.store.DB(), store.AuthKindWeb, hash)
	if err != nil {
		return 0, false
	}
	return userID, true
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var err error
	if r.FormValue("mode") == "weak" {
		_, err = store.RebuildTodayWeakSession(r.Context(), s.store.DB(), uid, 5)
	} else {
		_, err = store.EnsureTodaySession(r.Context(), s.store.DB(), uid, 5)
	}
	if err != nil {
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
	ProblemID      int64
	Slug           string
	LeetcodeID     string
	Title          string
	Difficulty     models.Difficulty
	URL            string
	Topics         []models.Tag
	Status         models.Status
	Completed      bool
	Journal        string
	MistakeTags    []string
	MistakeOptions []mistakeTagOption
}

type mistakeTagOption struct {
	Value   string
	Label   string
	Checked bool
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
		mistakeTags := []string{}
		detail, err := store.GetProblemDetail(ctx, s.store.DB(), uid, p.LeetcodeSlug, 1)
		if err == nil && len(detail.Attempts) > 0 {
			journal = detail.Attempts[0].Journal
			mistakeTags = normalizeMistakeTags(detail.Attempts[0].MistakeTags)
		}

		card.Problems = append(card.Problems, sessionProblem{
			ProblemID:      p.ID,
			Slug:           p.LeetcodeSlug,
			LeetcodeID:     p.LeetcodeFrontendID,
			Title:          p.Title,
			Difficulty:     p.Difficulty,
			URL:            p.URL,
			Topics:         p.TopicTags,
			Status:         up.Status,
			Completed:      completed,
			Journal:        journal,
			MistakeTags:    mistakeTags,
			MistakeOptions: mistakeOptionsForSelection(mistakeTags),
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

func normalizeMistakeTags(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[strings.ToLower(strings.TrimSpace(value))] = true
	}
	out := make([]string, 0, len(mistakeTaxonomy))
	for _, opt := range mistakeTaxonomy {
		if seen[opt.Value] {
			out = append(out, opt.Value)
		}
	}
	return out
}

func mistakeOptionsForSelection(selected []string) []mistakeTagOption {
	checked := make(map[string]bool, len(selected))
	for _, tag := range normalizeMistakeTags(selected) {
		checked[tag] = true
	}
	options := make([]mistakeTagOption, 0, len(mistakeTaxonomy))
	for _, opt := range mistakeTaxonomy {
		options = append(options, mistakeTagOption{
			Value:   opt.Value,
			Label:   opt.Label,
			Checked: checked[opt.Value],
		})
	}
	return options
}

var mistakeTaxonomy = []mistakeTagOption{
	{Value: "edge-case", Label: "Edge case"},
	{Value: "off-by-one", Label: "Off by one"},
	{Value: "wrong-invariant", Label: "Wrong invariant"},
	{Value: "complexity", Label: "Complexity"},
	{Value: "implementation-bug", Label: "Implementation bug"},
	{Value: "pattern-gap", Label: "Pattern gap"},
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

func (s *server) handleLists(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, web.AppPath(s.basePath, "/lists/blind-75"), http.StatusFound)
}

func (s *server) handleListDetail(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	lists, err := store.ListCuratedLists(r.Context(), s.store.DB(), uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	list, err := store.GetCuratedList(r.Context(), s.store.DB(), uid, slug)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	problems, err := store.ListCuratedListProblems(r.Context(), s.store.DB(), uid, slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.page(w, "list_detail", web.PageData{
		Title:   list.Name,
		UserID:  uid,
		NavItem: "lists",
		Data: listDetailPageData{
			List:         *list,
			Lists:        lists,
			SelectedSlug: slug,
			Sections:     groupBySection(problems),
		},
	})
}

type PatternSection struct {
	Name        string
	SolvedCount int
	TotalCount  int
	Problems    []store.CuratedListProblem
}

type listDetailPageData struct {
	List         store.CuratedList
	Lists        []store.CuratedList
	SelectedSlug string
	Sections     []PatternSection
}

func groupBySection(problems []store.CuratedListProblem) []PatternSection {
	var order []string
	groups := make(map[string]*PatternSection)
	for _, p := range problems {
		key := p.Section
		if key == "" && len(p.Topics) > 0 {
			key = p.Topics[0].Name
		}
		if key == "" {
			key = "Other"
		}
		if _, exists := groups[key]; !exists {
			order = append(order, key)
			groups[key] = &PatternSection{Name: key}
		}
		g := groups[key]
		g.Problems = append(g.Problems, p)
		g.TotalCount++
		if p.Status != models.StatusNew {
			g.SolvedCount++
		}
	}
	sections := make([]PatternSection, len(order))
	for i, k := range order {
		sections[i] = *groups[k]
	}
	return sections
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
	err = store.UpdateLatestAttemptReview(r.Context(), s.store.DB(), uid, problemID, r.FormValue("journal"), normalizeMistakeTags(r.Form["mistake_tags"]))
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

type extTodayProblem struct {
	Slug       string            `json:"slug"`
	LeetcodeID string            `json:"leetcode_id"`
	Title      string            `json:"title"`
	Difficulty models.Difficulty `json:"difficulty"`
	URL        string            `json:"url"`
	Completed  bool              `json:"completed"`
}

type extTodayProblemsResp struct {
	Problems       []extTodayProblem `json:"problems"`
	CompletedCount int               `json:"completed_count"`
	TotalCount     int               `json:"total_count"`
	Done           bool              `json:"done"`
}

func extTodayProblemResponseFromCard(card sessionCardData) extTodayProblemsResp {
	resp := extTodayProblemsResp{
		CompletedCount: card.CompletedCount,
		TotalCount:     card.TotalCount,
		Done:           card.Done,
	}
	for _, problem := range card.Problems {
		resp.Problems = append(resp.Problems, extTodayProblem{
			Slug:       problem.Slug,
			LeetcodeID: problem.LeetcodeID,
			Title:      problem.Title,
			Difficulty: problem.Difficulty,
			URL:        problem.URL,
			Completed:  problem.Completed,
		})
	}
	return resp
}

func (s *server) handleExtTodayProblems(w http.ResponseWriter, r *http.Request) {
	uid := auth.UserID(r.Context())
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
	card, err := s.sessionCard(r.Context(), uid, sess, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, extTodayProblemResponseFromCard(card))
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
