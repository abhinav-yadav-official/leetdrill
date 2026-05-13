// Package web holds template rendering and HTTP handlers for the browser-
// facing pages. Templates are embedded so the binary ships standalone.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed templates/*.html partials/*.html
var assetsFS embed.FS

// Renderer compiles every page template paired with the base layout, and
// every partial as a standalone template.
type Renderer struct {
	pages    map[string]*template.Template
	partials map[string]*template.Template
}

func NewRenderer() (*Renderer, error) {
	return NewRendererWithBasePath("")
}

func NewRendererWithBasePath(basePath string) (*Renderer, error) {
	basePath = CleanBasePath(basePath)
	funcs := template.FuncMap{
		"fmtTime":      fmtTime,
		"fmtRelTime":   fmtRelTime,
		"appPath":      func(target string) string { return AppPath(basePath, target) },
		"badgeClass":   badgeClassForDifficulty,
		"statusBadge":  badgeClassForStatus,
		"statusLabel":  labelForStatus,
		"verdictBadge": badgeClassForVerdict,
		"mistakeLabel": labelForMistakeTag,
		"upper":       strings.ToUpper,
		"hasItems":    func(n int) bool { return n > 0 },
		"ldLogo":      ldLogo,
		"progressPct": progressPct,
	}

	base, err := assetsFS.ReadFile("templates/_base.html")
	if err != nil {
		return nil, fmt.Errorf("read base: %w", err)
	}
	baseStr := string(base)

	partNames, err := fs.Glob(assetsFS, "partials/*.html")
	if err != nil {
		return nil, err
	}
	var partialBodies []string
	for _, name := range partNames {
		body, err := assetsFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read partial %s: %w", name, err)
		}
		partialBodies = append(partialBodies, string(body))
	}

	pages := map[string]*template.Template{}
	pageNames, err := fs.Glob(assetsFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, name := range pageNames {
		short := strings.TrimSuffix(strings.TrimPrefix(name, "templates/"), ".html")
		if strings.HasPrefix(short, "_") {
			continue
		}
		body, err := assetsFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read page %s: %w", name, err)
		}
		t, err := template.New(short).Funcs(funcs).Parse(baseStr)
		if err != nil {
			return nil, fmt.Errorf("parse base for %s: %w", short, err)
		}
		for _, part := range partialBodies {
			if _, err := t.Parse(part); err != nil {
				return nil, fmt.Errorf("parse partials for %s: %w", short, err)
			}
		}
		if _, err := t.Parse(string(body)); err != nil {
			return nil, fmt.Errorf("parse page %s: %w", short, err)
		}
		pages[short] = t
	}

	partials := map[string]*template.Template{}
	for _, name := range partNames {
		short := strings.TrimSuffix(strings.TrimPrefix(name, "partials/"), ".html")
		body, err := assetsFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		t, err := template.New(short).Funcs(funcs).Parse(string(body))
		if err != nil {
			return nil, fmt.Errorf("parse partial %s: %w", short, err)
		}
		partials[short] = t
	}

	return &Renderer{pages: pages, partials: partials}, nil
}

// PageData writes a page template using the base layout. Data is exposed as `.Data`.
type PageData struct {
	Title    string
	UserID   int64
	NavItem  string
	BasePath string
	Data     any
}

func (r *Renderer) Page(w http.ResponseWriter, name string, p PageData) {
	t, ok := r.pages[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", p); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

// Partial renders a fragment by name.
func (r *Renderer) Partial(w http.ResponseWriter, name string, data any) {
	t, ok := r.partials[name]
	if !ok {
		http.Error(w, "partial not found: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

// ---- template funcs ----

func AppPath(basePath, target string) string {
	basePath = CleanBasePath(basePath)
	target = strings.TrimSpace(target)
	if target == "" {
		target = "/"
	}
	if !strings.HasPrefix(target, "/") {
		target = "/" + target
	}
	if basePath == "" {
		return target
	}
	if target == "/" {
		return basePath + "/"
	}
	return basePath + target
}

func CleanBasePath(basePath string) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" || basePath == "/" {
		return ""
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	return strings.TrimRight(basePath, "/")
}

func fmtTime(t any) string {
	switch v := t.(type) {
	case time.Time:
		if v.IsZero() {
			return "—"
		}
		return v.UTC().Format("2006-01-02 15:04 UTC")
	case *time.Time:
		if v == nil || v.IsZero() {
			return "—"
		}
		return v.UTC().Format("2006-01-02 15:04 UTC")
	}
	return "—"
}

func fmtRelTime(t any) string {
	var ts time.Time
	switch v := t.(type) {
	case time.Time:
		ts = v
	case *time.Time:
		if v == nil {
			return "—"
		}
		ts = *v
	default:
		return "—"
	}
	if ts.IsZero() {
		return "—"
	}
	d := time.Since(ts)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	default:
		return ts.UTC().Format("2006-01-02")
	}
}

func badgeClassForDifficulty(d any) string {
	switch fmt.Sprint(d) {
	case "Easy":
		return "bg-green-100 text-green-800"
	case "Hard":
		return "bg-red-100 text-red-800"
	default:
		return "bg-yellow-100 text-yellow-800"
	}
}

func badgeClassForStatus(s any) string {
	switch fmt.Sprint(s) {
	case "new":
		return "bg-slate-200 text-slate-700"
	case "review":
		return "bg-sky-100 text-sky-800"
	case "mastered":
		return "bg-emerald-100 text-emerald-800"
	}
	return "bg-slate-100 text-slate-700"
}

func labelForStatus(s any) string {
	return fmt.Sprint(s)
}

func progressPct(solved, total int) int {
	if total == 0 {
		return 0
	}
	return (solved * 100) / total
}

func labelForMistakeTag(tag any) string {
	switch fmt.Sprint(tag) {
	case "edge-case":
		return "Edge case"
	case "off-by-one":
		return "Off by one"
	case "wrong-invariant":
		return "Wrong invariant"
	case "complexity":
		return "Complexity"
	case "implementation-bug":
		return "Implementation bug"
	case "pattern-gap":
		return "Pattern gap"
	default:
		return fmt.Sprint(tag)
	}
}

func ldLogo() template.HTML {
	return template.HTML(`<svg aria-label="LeetDrill logo" role="img" viewBox="0 0 64 64" class="h-8 w-8 shrink-0 rounded-md">
          <rect width="64" height="64" rx="12" fill="#18181b"></rect>
          <text x="32" y="39" text-anchor="middle" font-family="Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="24" font-weight="800" fill="#f4f4f5">LD</text>
        </svg>`)
}

func badgeClassForVerdict(v any) string {
	if fmt.Sprint(v) == "AC" {
		return "bg-emerald-100 text-emerald-800"
	}
	return "bg-rose-100 text-rose-800"
}
