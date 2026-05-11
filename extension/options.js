// LeetDrill options page.

const $ = (id) => document.getElementById(id);

function send(type, payload) {
  return new Promise((resolve) => {
    chrome.runtime.sendMessage({ type, payload }, (res) => resolve(res || { ok: false }));
  });
}

async function load() {
  const res = await send("LEETDRILL_GET_CONFIG");
  if (res.ok) {
    $("backend").value = res.data.backendUrl || "http://localhost:8080";
  }
}

function setStatus(msg, cls) {
  const el = $("status");
  el.textContent = msg;
  el.className = "status " + (cls || "");
}

$("save").addEventListener("click", async () => {
  const res = await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
  setStatus(res.ok ? "saved" : "save failed", res.ok ? "ok" : "bad");
});

$("connect").addEventListener("click", async () => {
  await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
  const payload = {};
  const email = $("email").value.trim();
  const pw = $("password").value;
  if (email && pw) {
    payload.email = email;
    payload.password = pw;
  }
  const res = await send("LEETDRILL_HANDSHAKE", payload);
  if (res.ok) {
    setStatus("connected ✓ token saved", "ok");
    $("password").value = "";
  } else {
    setStatus("connect failed: " + (res.error || "unknown"), "bad");
  }
});

load();
