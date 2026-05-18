# Abhiy Devportfolio-Style Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current dark portfolio with a light devportfolio-inspired static homepage.

**Architecture:** Keep the homepage as one static HTML document with inline CSS under `web/homepage/index.html`. Static validation remains in `scripts/check_homepage.py`.

**Tech Stack:** HTML, CSS, Python static checker, Playwright screenshots, Go tests.

---

### Task 1: Static Contract

**Files:**
- Modify: `scripts/check_homepage.py`

- [ ] Require light color scheme, IBM Plex Mono, `Hello!`, `About Me`, `Education`, and `devportfolio-inspired` marker.
- [ ] Preserve existing requirements for resume, contact links, LeetDrill, archive repos, animation, reduced motion, and mobile breakpoints.
- [ ] Run `python3 scripts/check_homepage.py` and confirm it fails before the HTML is replaced.

### Task 2: Static Page Redesign

**Files:**
- Modify: `web/homepage/index.html`

- [ ] Replace the dark editorial layout with a light devportfolio-inspired layout.
- [ ] Add hero code-symbol/grid background, social/resume actions, About, Projects, Systems, Archive, Experience, Education, and Footer sections.
- [ ] Keep LeetDrill as the only featured project and keep other repos in the archive.
- [ ] Keep responsive breakpoints and reduced-motion handling.

### Task 3: Verification And Deploy

**Files:**
- Verify: `scripts/check_homepage.py`
- Verify: `web/homepage/index.html`

- [ ] Run `python3 scripts/check_homepage.py`.
- [ ] Run HTML parser sanity check.
- [ ] Run Playwright mobile and desktop screenshots.
- [ ] Run `go test ./...`.
- [ ] Deploy with `task deploy:server -- abhiy.xyz`.
- [ ] Verify live HTML, resume PDF, and LeetDrill health.
