# LeetDrill Companion (extension)

Chrome MV3 and Firefox WebExtension companion for https://abhiy.xyz/leetdrill.
Main flows:

1. Captures submission verdicts from leetcode.com via a `window.fetch` hook on
   the `/submissions/detail/<id>/check/` polling endpoint.
2. Syncs `LEETCODE_SESSION` + `csrftoken` cookies to the backend every 6h so
   the backend can run authed GraphQL queries (sync worker).
3. Connects with the user's existing LeetDrill browser login when available.
4. Triggers a cold-start history import for the connected backend user.

## Load locally: Chrome

1. Visit `chrome://extensions/`, enable Developer mode.
2. Click "Load unpacked", point at this directory.
3. Sign in at `https://abhiy.xyz/leetdrill`, then open the popup or options page.
   The extension will use the existing browser login.
4. Open a LeetCode problem and submit. Console should log
   `[leetdrill] submission applied`.
5. Click **import history** in the popup after cookies sync to backfill old
   accepted submissions for this LeetDrill user.

## Load locally: Firefox

1. Visit `about:debugging#/runtime/this-firefox`.
2. Click **Load Temporary Add-on...**.
3. Select `extension/firefox/manifest.json`.
4. Sign in at `https://abhiy.xyz/leetdrill`, then open the popup or options page.
   The extension will use the existing browser login.
5. Open a LeetCode problem and submit. Console should log
   `[leetdrill] submission applied`.

## Build share packages

```bash
task extension:package
```

This writes Chrome/Edge and Firefox packages to `dist/extension-share/`.

To publish them to the VPS share page:

```bash
task extension:deploy
```

The public page is `https://abhiy.xyz/shared/leetdrill-extension/`.
Submit the Chrome zip to the Chrome Web Store and the Firefox XPI/source zip to
addons.mozilla.org for signing. Store listing and permission text lives in
`STORE_LISTING.md`; privacy text lives in `PRIVACY.md`.

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
