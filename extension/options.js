// LeetDrill options page.

const $ = (id) => document.getElementById(id);

function send(type, payload) {
  return ldx.runtime
    .sendMessage({ type, payload })
    .then((res) => res || { ok: false })
    .catch((err) => ({ ok: false, error: err.message || String(err) }));
}

async function load() {
  const res = await send("LEETDRILL_GET_CONFIG");
  if (res.ok) {
    $("backend").value = res.data.backendUrl || "https://abhiy.xyz/leetdrill";
  }
}

function setStatus(msg, cls) {
  const el = $("status");
  el.textContent = msg;
  el.className = "status " + (cls || "");
}

function statusText(data) {
  if (!data) return "connection status unavailable";
  if (data.token) return `connected to ${data.backendUrl}`;
  if (data.webSession) {
    const path = data.cookiePath ? ` at ${data.cookiePath}` : "";
    return `browser login found${path}; click use browser login`;
  }
  return "no browser login cookie found; sign in to abhiy.xyz/leetdrill in this same browser profile";
}

async function refreshStatus() {
  const res = await send("LEETDRILL_CONNECT_STATUS");
  if (!res.ok) {
    setStatus(res.error || "connection status unavailable", "bad");
    return;
  }
  setStatus(statusText(res.data), res.data.token || res.data.webSession ? "ok" : "bad");
}

$("save").addEventListener("click", async () => {
  const res = await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
  setStatus(res.ok ? "saved" : "save failed", res.ok ? "ok" : "bad");
});

$("check").addEventListener("click", async () => {
  await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
  await refreshStatus();
});

$("openApp").addEventListener("click", async () => {
  await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
  const res = await send("LEETDRILL_OPEN_APP");
  setStatus(res.ok ? "opened LeetDrill" : `open failed: ${res.error || "unknown error"}`, res.ok ? "ok" : "bad");
});

$("testConnection").addEventListener("click", async () => {
  await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
  const res = await send("LEETDRILL_TEST_CONNECTION");
  if (!res.ok) {
    setStatus(`test failed: ${res.error || "unknown error"}`, "bad");
    return;
  }
  const data = res.data || {};
  if (data.connected) {
    setStatus("connection works: Zen can reach abhiy.xyz with the saved token", "ok");
  } else if (data.permission === "blocked") {
    setStatus(`Zen blocked abhiy.xyz access: ${data.message || "fetch failed"}`, "bad");
  } else {
    setStatus(`connection failed: ${data.message || "token missing or rejected"}`, "bad");
  }
});

$("saveToken").addEventListener("click", async () => {
  const res = await send("LEETDRILL_SAVE_TOKEN", { token: $("manualToken").value.trim() });
  if (res.ok) {
    $("manualToken").value = "";
    const test = await send("LEETDRILL_TEST_CONNECTION");
    if (test.ok && test.data && test.data.connected) {
      setStatus("connected - manual code saved and verified", "ok");
    } else {
      setStatus(`manual code saved; test failed: ${test.data && test.data.message ? test.data.message : "run test connection"}`, "bad");
    }
  } else {
    setStatus(`manual connect failed: ${res.error || "unknown error"}`, "bad");
  }
});

$("connect").addEventListener("click", async () => {
  await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
  const payload = {};
  const email = $("email").value.trim();
  const pw = $("password").value;
  if (email && pw) {
    payload.email = email;
    payload.password = pw;
    const res = await send("LEETDRILL_HANDSHAKE", payload);
    if (res.ok) {
      setStatus("connected - token saved", "ok");
      $("password").value = "";
    } else {
      setStatus(`connect failed: ${res.error || "unknown error"}`, "bad");
    }
  } else {
    const res = await send("LEETDRILL_OPEN_WEB_CONNECT");
    setStatus(
      res.ok ? "opened LeetDrill connect tab" : `connect failed: ${res.error || "unknown error"}`,
      res.ok ? "ok" : "bad"
    );
  }
});

load().then(refreshStatus);
