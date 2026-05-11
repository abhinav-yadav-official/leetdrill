// LeetDrill background runtime.
//
// Responsibilities:
//   1. Bootstrap: pull a long-lived token from /api/ext/handshake on install
//      (or when the user clicks "Connect" in the options page).
//   2. Cookie sync: every 6h, read LEETCODE_SESSION + csrftoken via extension cookies
//      and POST to /api/ext/cookies.
//   3. Submission relay: content.js forwards submission payloads here; we POST
//      them to /api/ext/submission and broadcast the verdict back to any open
//      popup/page that cares.
//   4. Auth recovery: on 401 from the backend, clear the saved token and
//      surface a notice via the extension action badge.

if (typeof importScripts === "function" && typeof ldx === "undefined") {
  importScripts("compat.js");
}

const COOKIE_ALARM = "leetdrill-cookie-sync";
const COOKIE_PERIOD_MIN = 6 * 60; // 6 hours

const DEFAULTS = {
  backendUrl: "https://abhiy.xyz/leetdrill",
  token: ""
};

async function getConfig() {
  const stored = await ldx.storage.get(DEFAULTS);
  return { ...DEFAULTS, ...stored };
}

async function saveConfig(patch) {
  await ldx.storage.set(patch);
}

function authHeaders(token) {
  const h = { "Content-Type": "application/json" };
  if (token) h["Authorization"] = `Bearer ${token}`;
  return h;
}

async function apiPost(path, body) {
  const { backendUrl, token } = await getConfig();
  const url = `${backendUrl.replace(/\/$/, "")}${path}`;
  const res = await fetch(url, {
    method: "POST",
    headers: authHeaders(token),
    body: JSON.stringify(body || {})
  });
  if (res.status === 401) {
    await saveConfig({ token: "" });
    await ldx.action.setBadgeText({ text: "!" });
    await ldx.action.setBadgeBackgroundColor({ color: "#dc2626" });
    throw new Error("unauthorized — re-handshake");
  }
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`HTTP ${res.status}: ${text}`);
  }
  return res.json().catch(() => ({}));
}

async function apiGet(path) {
  const { backendUrl, token } = await getConfig();
  const url = `${backendUrl.replace(/\/$/, "")}${path}`;
  const res = await fetch(url, { method: "GET", headers: authHeaders(token) });
  if (res.status === 401) {
    await saveConfig({ token: "" });
    await ldx.action.setBadgeText({ text: "!" });
    throw new Error("unauthorized");
  }
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`HTTP ${res.status}: ${text}`);
  }
  return res.json();
}

async function handshake({ email, password } = {}) {
  const { backendUrl } = await getConfig();
  const url = `${backendUrl.replace(/\/$/, "")}/api/ext/handshake`;
  const body = email && password ? { email, password } : {};
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  });
  if (!res.ok) throw new Error(`handshake HTTP ${res.status}`);
  const data = await res.json();
  if (!data.token) throw new Error("handshake response missing token");
  await saveConfig({ token: data.token });
  await ldx.action.setBadgeText({ text: "" });
  return data;
}

async function readLeetCodeCookies() {
  const session = await ldx.cookies.get({
    url: "https://leetcode.com",
    name: "LEETCODE_SESSION"
  });
  const csrf = await ldx.cookies.get({
    url: "https://leetcode.com",
    name: "csrftoken"
  });
  return {
    leetcode_session: session ? session.value : "",
    csrf_token: csrf ? csrf.value : ""
  };
}

async function syncCookies() {
  const { leetcode_session, csrf_token } = await readLeetCodeCookies();
  if (!leetcode_session || !csrf_token) {
    console.warn("[leetdrill] cookies missing — user not logged in to leetcode");
    return;
  }
  await apiPost("/api/ext/cookies", { leetcode_session, csrf_token });
  console.log("[leetdrill] cookies synced");
}

// ---- lifecycle ----

ldx.runtime.onInstalled.addListener(async () => {
  await ldx.alarms.create(COOKIE_ALARM, { periodInMinutes: COOKIE_PERIOD_MIN });
  // Attempt single-user handshake right away. Fails harmlessly in multi-user
  // mode; user opens the options page to enter credentials.
  try {
    await handshake();
  } catch (e) {
    console.log("[leetdrill] initial handshake skipped:", e.message);
  }
  try {
    await syncCookies();
  } catch (e) {
    console.log("[leetdrill] initial cookie sync skipped:", e.message);
  }
});

ldx.alarms.onAlarm.addListener(async (alarm) => {
  if (alarm.name === COOKIE_ALARM) {
    try {
      await syncCookies();
    } catch (e) {
      console.warn("[leetdrill] alarm cookie sync failed:", e.message);
    }
  }
});

// ---- messages from content/popup/options ----

ldx.runtime.onMessage.addListener((msg, sender) =>
  (async () => {
    try {
      switch (msg && msg.type) {
        case "LEETDRILL_SUBMISSION": {
          const data = await apiPost("/api/ext/submission", msg.payload);
          return { ok: true, data };
        }
        case "LEETDRILL_SYNC_COOKIES": {
          await syncCookies();
          return { ok: true };
        }
        case "LEETDRILL_HANDSHAKE": {
          const data = await handshake(msg.payload || {});
          return { ok: true, data };
        }
        case "LEETDRILL_NEXT_PROBLEM": {
          const data = await apiGet("/api/ext/next-problem");
          return { ok: true, data };
        }
        case "LEETDRILL_COLD_START": {
          const data = await apiPost("/api/ext/cold-start", msg.payload || {});
          return { ok: true, data };
        }
        case "LEETDRILL_GET_CONFIG": {
          return { ok: true, data: await getConfig() };
        }
        case "LEETDRILL_SAVE_CONFIG": {
          await saveConfig(msg.payload || {});
          return { ok: true };
        }
        default:
          return { ok: false, error: "unknown message" };
      }
    } catch (err) {
      return { ok: false, error: err.message || String(err) };
    }
  })()
);
