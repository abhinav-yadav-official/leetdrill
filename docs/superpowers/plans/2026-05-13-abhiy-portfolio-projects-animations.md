# Abhiy Portfolio Projects And Animations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the static `abhiy.xyz` portfolio with CSS animations and more GitHub project content.

**Architecture:** Keep the homepage as one static HTML file under `web/homepage`. Use `scripts/check_homepage.py` as the static contract for required content and CSS markers.

**Tech Stack:** HTML, CSS, Python static checker, Playwright screenshots, Go regression suite.

---

### Task 1: Static Contract

**Files:**
- Modify: `scripts/check_homepage.py`

- [ ] Add checks for animation CSS markers: `@keyframes`, `prefers-reduced-motion`, `animation:`, and `transition:`.
- [ ] Add checks for the new featured and archive project links.
- [ ] Run `python3 scripts/check_homepage.py` and confirm it fails before the HTML is expanded.

### Task 2: Homepage Expansion

**Files:**
- Modify: `web/homepage/index.html`

- [ ] Add CSS-only animations and reduced-motion fallback.
- [ ] Add featured project cards for LeetDrill, manager.ai, legacy-mac-wheels, and homebrew-legacy.
- [ ] Add project archive cards for the remaining public repositories.
- [ ] Add the systems section.
- [ ] Keep mobile breakpoints and overflow protections.

### Task 3: Verification And Deploy

**Files:**
- Verify: `scripts/check_homepage.py`
- Verify: `web/homepage/index.html`

- [ ] Run `python3 scripts/check_homepage.py`.
- [ ] Run `bash -n scripts/deploy_server.sh`.
- [ ] Render Playwright screenshots at desktop and 390px mobile.
- [ ] Run `go test ./...`.
- [ ] Deploy with `task deploy:server -- abhiy.xyz`.
- [ ] Verify live HTML, resume PDF, and LeetDrill health.
