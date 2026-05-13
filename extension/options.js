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
