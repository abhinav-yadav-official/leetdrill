# LeetDrill Companion (extension)

Chrome MV3 extension. Two flows:

1. Captures submission verdicts from leetcode.com via a `window.fetch` hook on
   the `/submissions/detail/<id>/check/` polling endpoint.
2. Syncs `LEETCODE_SESSION` + `csrftoken` cookies to the backend every 6h so
   the backend can run authed GraphQL queries (sync worker).

## Load locally

1. Visit `chrome://extensions/`, enable Developer mode.
2. Click "Load unpacked", point at this directory.
3. Open the extension's options page, set Backend URL (default
   `http://localhost:8080`), click **Connect**.
   - Single-user backend: leave email/password blank.
   - Multi-user backend: fill both.
4. Open a LeetCode problem and submit. Console should log
   `[leetdrill] submission applied`.

## Files

- `manifest.json` — MV3 manifest (cookies + storage perms; host perms for
  leetcode.com + localhost backends).
- `background.js` — service worker. API client, cookie alarm, handshake.
- `content.js` — runs in isolated world on `/problems/*`. Injects inject.js,
  forwards verdict payloads.
- `inject.js` — page-context. Wraps `window.fetch`, posts verdict messages.
- `popup.html` + `popup.js` — connection status, "open next problem" button.
- `options.html` + `options.js` — backend URL + handshake.

## Icons

Not included. Drop `icon16.png`, `icon48.png`, `icon128.png` into this
directory before publishing; loaded-unpacked works without them.
