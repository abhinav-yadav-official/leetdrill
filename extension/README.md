# LeetDrill Companion (extension)

Chrome MV3 and Firefox WebExtension companion. Main flows:

1. Captures submission verdicts from leetcode.com via a `window.fetch` hook on
   the `/submissions/detail/<id>/check/` polling endpoint.
2. Syncs `LEETCODE_SESSION` + `csrftoken` cookies to the backend every 6h so
   the backend can run authed GraphQL queries (sync worker).
3. Triggers a cold-start history import for the connected backend user.

## Load locally: Chrome

1. Visit `chrome://extensions/`, enable Developer mode.
2. Click "Load unpacked", point at this directory.
3. Open the extension's options page, set Backend URL (default
   `https://abhiy.xyz/leetdrill`), click **Connect**.
   - Single-user backend: leave email/password blank.
   - Multi-user backend: fill both.
4. Open a LeetCode problem and submit. Console should log
   `[leetdrill] submission applied`.
5. Click **import history** in the popup after cookies sync to backfill old
   accepted submissions for this LeetDrill user.

## Load locally: Firefox

1. Visit `about:debugging#/runtime/this-firefox`.
2. Click **Load Temporary Add-on...**.
3. Select `extension/firefox/manifest.json`.
4. Open the extension options page, set Backend URL if needed, and click
   **Connect**.
   - Multi-user backend: fill email/password.
   - Single-user backend: leave email/password blank.
5. Open a LeetCode problem and submit. Console should log
   `[leetdrill] submission applied`.

## Files

- `manifest.json` — Chrome MV3 manifest.
- `firefox/manifest.json` — Firefox MV2 manifest using the same extension UI
  and scripts copied into `firefox/`.
- `compat.js` — small Chrome/Firefox WebExtension API wrapper.
- `background.js` — background runtime. API client, cookie alarm, handshake.
- `content.js` — runs in isolated world on `/problems/*`. Injects inject.js,
  forwards verdict payloads.
- `inject.js` — page-context. Wraps `window.fetch`, posts verdict messages.
- `popup.html` + `popup.js` — connection status, "open next problem" button.
- `options.html` + `options.js` — backend URL + handshake.

## Icons

Not included. Add an `icons` block to `manifest.json` when `icon16.png`,
`icon48.png`, and `icon128.png` exist.
