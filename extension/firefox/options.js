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

let tokenSaveTimer = null;

function scheduleTokenSave() {
  clearTimeout(tokenSaveTimer);
  tokenSaveTimer = setTimeout(saveLoginToken, 350);
}

async function saveLoginToken() {
  const token = $("manualToken").value.trim();
  if (!token) return;
  await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
  const res = await send("LEETDRILL_SAVE_TOKEN", { token });
  if (!res.ok) {
    setStatus(`login token failed: ${res.error || "unknown error"}`, "bad");
    return;
  }
  $("manualToken").value = "";
  const test = await send("LEETDRILL_TEST_CONNECTION");
  if (test.ok && test.data && test.data.connected) {
    setStatus("connected - login token saved and verified", "ok");
  } else {
    const msg = test.data && test.data.message ? test.data.message : "run test connection";
    setStatus(`login token saved; test failed: ${msg}`, "bad");
  }
}

$("codePage").addEventListener("click", async () => {
  await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
  const res = await send("LEETDRILL_OPEN_CODE_PAGE");
  setStatus(res.ok ? "opened login code page" : `open failed: ${res.error || "unknown error"}`, res.ok ? "ok" : "bad");
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
    setStatus("connection works: extension can reach abhiy.xyz with the saved code", "ok");
  } else if (data.permission === "blocked") {
    setStatus(`browser blocked abhiy.xyz access: ${data.message || "fetch failed"}`, "bad");
  } else {
    setStatus(`connection failed: ${data.message || "code missing or rejected"}`, "bad");
  }
});

$("manualToken").addEventListener("input", scheduleTokenSave);
$("manualToken").addEventListener("paste", scheduleTokenSave);

load();
