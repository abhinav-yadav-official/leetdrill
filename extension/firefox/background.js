// LeetDrill background runtime.
//
// Responsibilities:
//   1. Bootstrap: pull a long-lived token from /api/ext/handshake on install
//      using the existing LeetDrill web login when available.
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
  await ensureConnected();
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
  await ensureConnected();
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

function normalizeBackendUrl(raw) {
  return (raw || DEFAULTS.backendUrl).replace(/\/$/, "");
}

function backendURLCandidates(backendUrl) {
  try {
    const normalized = normalizeBackendUrl(backendUrl);
    const url = new URL(normalized);
    const appURL = new URL(normalized + "/");
    return [...new Set([
      appURL.href,
      url.href,
      `${url.origin}/`
    ])];
  } catch (_) {
    return [];
  }
}

function backendHostname(backendUrl) {
  try {
    return new URL(normalizeBackendUrl(backendUrl)).hostname;
  } catch (_) {
    return "";
  }
}

function bestCookie(cookies) {
  return (cookies || [])
    .filter((cookie) => cookie && cookie.name === "ld_session")
    .sort((a, b) => {
      const pathDelta = (b.path || "").length - (a.path || "").length;
      if (pathDelta) return pathDelta;
      return Number(Boolean(b.secure)) - Number(Boolean(a.secure));
    })[0] || null;
}

async function findBackendSessionCookie(backendUrl) {
  for (const url of backendURLCandidates(backendUrl)) {
    const cookie = await ldx.cookies.get({ url, name: "ld_session" });
    if (cookie) return { cookie, source: url };
  }

  const domain = backendHostname(backendUrl);
  if (domain && ldx.cookies.getAll) {
    const cookie = bestCookie(await ldx.cookies.getAll({ domain, name: "ld_session" }));
    if (cookie) return { cookie, source: `domain:${domain}` };
  }

  return { cookie: null, source: "" };
}

async function readBackendSessionToken(backendUrl) {
  const found = await findBackendSessionCookie(backendUrl);
  return found.cookie ? found.cookie.value : "";
}

async function connectionStatus() {
  const cfg = await getConfig();
  const found = await findBackendSessionCookie(cfg.backendUrl);
  return {
    backendUrl: cfg.backendUrl,
    token: Boolean(cfg.token),
    webSession: Boolean(found.cookie),
    cookieDomain: found.cookie ? found.cookie.domain || "" : "",
    cookiePath: found.cookie ? found.cookie.path || "" : "",
    cookieSource: found.source
  };
}

function isTrustedExtensionConnectSender(sender) {
  try {
    const rawURL = (sender && sender.url) ||
      (sender && sender.tab && sender.tab.url) ||
      "";
    const url = new URL(rawURL);
    return url.protocol === "https:" &&
      url.hostname === "abhiy.xyz" &&
      url.pathname === "/leetdrill/extension/connect";
  } catch (_) {
    return false;
  }
}

async function saveExtensionTokenFromPage(token, sender) {
  if (!isTrustedExtensionConnectSender(sender)) {
    throw new Error("untrusted extension connect page");
  }
  const cleanToken = String(token || "").trim();
  if (!cleanToken) throw new Error("extension token missing");
  await saveConfig({ token: cleanToken });
  await ldx.action.setBadgeText({ text: "" });
}

async function saveManualExtensionToken(token) {
  const cleanToken = String(token || "").trim();
  if (!cleanToken) throw new Error("extension token missing");
  await saveConfig({ token: cleanToken });
  await ldx.action.setBadgeText({ text: "" });
}

async function openWebConnect() {
  const cfg = await getConfig();
  const url = `${normalizeBackendUrl(cfg.backendUrl)}/extension/connect`;
  await ldx.tabs.create({ url });
  return { url };
}

async function handshake({ email, password } = {}) {
  const { backendUrl } = await getConfig();
  const url = `${normalizeBackendUrl(backendUrl)}/api/ext/handshake`;
  const body = email && password ? { email, password } : {};
  let foundWebSession = false;
  if (!email && !password) {
    const webSessionToken = await readBackendSessionToken(backendUrl);
    if (webSessionToken) {
      foundWebSession = true;
      body.web_session_token = webSessionToken;
    }
  }
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify(body)
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    if (!email && !password && !foundWebSession && res.status === 400) {
      throw new Error("no LeetDrill login cookie found; open https://abhiy.xyz/leetdrill in this same browser profile, sign in, then retry");
    }
    throw new Error(`handshake HTTP ${res.status}${text ? `: ${text}` : ""}`);
  }
  const data = await res.json();
  if (!data.token) throw new Error("handshake response missing token");
  await saveConfig({ token: data.token });
  await ldx.action.setBadgeText({ text: "" });
  return data;
}

async function ensureConnected() {
  const cfg = await getConfig();
  if (cfg.token) return cfg;
  let lastConnectError = "";
  try {
    await handshake();
  } catch (err) {
    lastConnectError = err.message || String(err);
  }
  return { ...(await getConfig()), lastConnectError };
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
  // Try existing LeetDrill web login or single-user mode right away.
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
        case "LEETDRILL_ENSURE_CONNECTED": {
          return { ok: true, data: await ensureConnected() };
        }
        case "LEETDRILL_OPEN_WEB_CONNECT": {
          return { ok: true, data: await openWebConnect() };
        }
        case "LEETDRILL_EXTENSION_TOKEN": {
          await saveExtensionTokenFromPage((msg.payload || {}).token, sender);
          return { ok: true };
        }
        case "LEETDRILL_SAVE_TOKEN": {
          await saveManualExtensionToken((msg.payload || {}).token);
          return { ok: true };
        }
        case "LEETDRILL_CONNECT_STATUS": {
          return { ok: true, data: await connectionStatus() };
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
