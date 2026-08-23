<div align="center">

# LeetDrill

**Self-hosted LeetCode practice tracker with a spaced-repetition daily queue.**

[![Live App](https://img.shields.io/badge/Live%20App-abhiyadav.in%2Fleetdrill-2ea44f?style=for-the-badge)](https://abhiyadav.in/leetdrill)
[![Release](https://img.shields.io/github/v/release/almostturingcomplete/LeetDrill?style=for-the-badge)](https://github.com/almostturingcomplete/LeetDrill/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue?style=for-the-badge)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go&logoColor=white)](go.mod)

</div>

![LeetDrill](docs/screenshot.png)

## Overview

LeetDrill imports the LeetCode catalog, captures your accepted submissions through a browser extension, and builds a daily practice queue from what is due, unsolved, or needs more work — so you review the right problems at the right time instead of grinding at random.

## Features

- **Catalog import** — pulls the LeetCode problem set into a local database.
- **Submission capture** — a browser extension records your accepted submissions automatically.
- **Spaced-repetition queue** — schedules reviews with the SM-2 algorithm (see [Concepts](#concepts)).
- **Daily workspace** — one focused view: what is due, attempt signal, and the review plan.
- **Self-hosted** — single Go binary + Postgres; your data stays yours.

## Live Access

- App: https://abhiyadav.in/leetdrill

## Installation

Prereqs: Go 1.25+, Docker, [Task](https://taskfile.dev).

```sh
git clone https://github.com/almostturingcomplete/LeetDrill.git
cd LeetDrill
cp .env.example .env
# set encryption key: openssl rand -base64 32  -> LEETDRILL_COOKIE_KEY
task install:tools
task db:up
task migrate:up
task test
task dev
```

Then load the browser extension from `extension/` to start capturing submissions.

## Usage

1. Sign in (email or Google).
2. Install the extension; solve problems on LeetCode — accepted submissions sync automatically.
3. Each day, work the queue ordered by what is due.

## Concepts

- **SM-2 (SuperMemo 2)** — the spaced-repetition algorithm behind the review queue. Each problem keeps an *ease factor* and an *interval*; when you review, the interval grows (good recall) or resets (poor recall), and the ease factor adjusts. Reviews are scheduled at expanding gaps so you revisit a problem just before you would forget it — maximising retention per minute spent.

## License

[MIT](LICENSE) © 2026 Abhinav Yadav
