# Abhiy Portfolio Homepage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a polished static portfolio homepage for `abhiy.xyz` with resume and project links.

**Architecture:** The public root remains a static page under `web/homepage`. Static checks guard required links/content, and deployment publishes every homepage asset to `/var/www/html/`.

**Tech Stack:** HTML, CSS, Python homepage checker, Bash deploy script.

---

### Task 1: Homepage Static Contract

**Files:**
- Modify: `scripts/check_homepage.py`
- Test: `python3 scripts/check_homepage.py`

- [ ] **Step 1: Write the failing static checks**

Require the homepage to include portfolio identity, resume link, LeetDrill links, public contact links, and the local resume PDF asset.

- [ ] **Step 2: Run the checker to verify it fails**

Run: `python3 scripts/check_homepage.py`

Expected: FAIL because the current homepage does not link `Abhinav_Resume.pdf` and does not include the new portfolio content.

### Task 2: Homepage Asset and Page

**Files:**
- Create: `web/homepage/Abhinav_Resume.pdf`
- Modify: `web/homepage/index.html`
- Test: `python3 scripts/check_homepage.py`

- [ ] **Step 1: Copy resume asset**

Copy `~/Abhinav_Resume.pdf` to `web/homepage/Abhinav_Resume.pdf`.

- [ ] **Step 2: Replace the homepage HTML**

Implement the approved editorial profile layout with hero, contact actions, LeetDrill project links, experience summary, and skills.

- [ ] **Step 3: Run the checker to verify it passes**

Run: `python3 scripts/check_homepage.py`

Expected: PASS with no output.

### Task 3: Deploy Homepage Assets

**Files:**
- Modify: `scripts/deploy_server.sh`
- Test: `bash -n scripts/deploy_server.sh`

- [ ] **Step 1: Update deploy publishing**

Change homepage publishing from uploading only `index.html` to syncing all files under `web/homepage/`, so `Abhinav_Resume.pdf` deploys with the page.

- [ ] **Step 2: Validate shell syntax**

Run: `bash -n scripts/deploy_server.sh`

Expected: PASS with no output.

### Task 4: Final Verification

**Files:**
- Verify: `scripts/check_homepage.py`
- Verify: Go test suite

- [ ] **Step 1: Run static homepage checks**

Run: `python3 scripts/check_homepage.py`

Expected: PASS with no output.

- [ ] **Step 2: Run backend tests**

Run: `go test ./...`

Expected: PASS for all packages.
