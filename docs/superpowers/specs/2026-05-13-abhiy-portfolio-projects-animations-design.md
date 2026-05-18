# Abhiy Portfolio Projects And Animations Design

## Goal

Make the dark `abhiy.xyz` portfolio feel more complete by adding restrained animation and a longer project-focused body.

## Scope

- Keep the existing dark editorial hero, resume link, contact links, experience, and stack sections.
- Add CSS-only animations: hero entrance, card reveal, hover lift, animated background movement, and reduced-motion fallback.
- Add more GitHub projects:
  - Featured: LeetDrill, manager.ai, legacy-mac-wheels, homebrew-legacy.
  - Archive: dotfiles, register-page, object-detect, doc-scan, mern-user-authentication, angular-freehand-drawing-app, keras-flask-app, react-clock-app.
- Add a `Systems I work on` section covering ranking/search, async jobs, caching, migrations, and observability/security.
- Keep the page static, mobile-friendly, and deployable from `web/homepage`.

## Verification

- `scripts/check_homepage.py` must protect required project links and animation markers.
- Render desktop and 390px mobile screenshots with Playwright.
- Run `go test ./...`.
- Deploy with `task deploy:server -- abhiy.xyz` and verify live HTML, resume PDF, and LeetDrill health.
