# LeetDrill Companion Store Submission

## Summary

LeetDrill Companion connects LeetCode activity to the practice tracker at
https://abhiy.xyz/leetdrill.

## Purpose

The extension has one purpose: keep the user's LeetDrill practice tracker up to
date from LeetCode. It captures final LeetCode submission results on problem
pages, syncs the user's LeetCode session cookies to the user's LeetDrill account,
and lets LeetDrill import solved-history data for daily practice planning.

## Description

LeetDrill Companion is for users of https://abhiy.xyz/leetdrill. After the user
signs in to LeetDrill in the browser, the extension can connect using that
existing login and stores a local extension token.

On LeetCode problem pages, the extension detects final submission results from
LeetCode's submission-check response and sends the problem slug, verdict, runtime,
memory, language, code, submission id, and timing metadata to LeetDrill. It can
also sync the user's LeetCode cookies to LeetDrill so the server can import the
user's solved history through LeetCode's authenticated APIs.

The extension does not inject ads, change search, track browsing outside
LeetCode problem pages, sell data, or send data to third parties.

## Permission Justifications

- `storage`: stores the LeetDrill backend URL and extension token locally.
- `cookies`: reads `LEETCODE_SESSION`, `csrftoken`, and the LeetDrill login cookie
  so the user can connect without re-entering credentials and sync LeetCode data
  to their own LeetDrill account.
- `alarms`: periodically syncs LeetCode cookies so imports keep working without
  repeated manual setup.
- `https://leetcode.com/*`: runs on LeetCode problem pages and reads LeetCode
  cookies needed for authenticated history import.
- `https://abhiy.xyz/*`: connects only to the LeetDrill backend and reads the
  LeetDrill login cookie for existing-login connection.

## Data Use

Data sent to LeetDrill:

- LeetCode submission result data from problem pages.
- LeetCode session cookies, only when the user clicks sync/import or during the
  periodic cookie sync.
- LeetDrill login cookie value, sent only to the same LeetDrill backend to mint
  an extension token.

Data is used only to maintain the user's LeetDrill practice tracker.

## Review Notes

- No remote JavaScript is loaded by the extension.
- The only injected script is bundled as `inject.js` and declared as a web
  accessible resource so it can observe page-context `fetch` responses on
  LeetCode problem pages.
- Host permissions are limited to LeetCode and the production LeetDrill backend.
- The extension can be tested by signing in at https://abhiy.xyz/leetdrill,
  installing the package, opening the popup, and visiting a LeetCode problem page.
