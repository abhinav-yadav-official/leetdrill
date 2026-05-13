// LeetDrill popup. Shows connection controls or today's full problem list.

function send(type, payload) {
  return ldx.runtime
    .sendMessage({ type, payload })
    .then((res) => res || { ok: false })
    .catch((err) => ({ ok: false, error: err.message || String(err) }));
}

const $ = (id) => document.getElementById(id);

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

async function saveBackend() {
  await send("LEETDRILL_SAVE_CONFIG", { backendUrl: $("backend").value.trim() });
}

async function saveLoginToken() {
  const token = $("manualToken").value.trim();
  if (!token) return;
  await saveBackend();
  const res = await send("LEETDRILL_SAVE_TOKEN", { token });
  if (!res.ok) {
    setStatus(`login token failed: ${res.error || "unknown error"}`, "bad");
    return;
  }
  $("manualToken").value = "";
  const test = await send("LEETDRILL_TEST_CONNECTION");
  if (test.ok && test.data && test.data.connected) {
    await showToday();
  } else {
    const msg = test.data && test.data.message ? test.data.message : "run test";
    setStatus(`login token saved; test failed: ${msg}`, "bad");
  }
}

async function showConnectPanel(cfg) {
  $("connectPanel").style.display = "block";
  $("todayPanel").style.display = "none";
  $("backend").value = (cfg && cfg.backendUrl) || "https://abhiy.xyz/leetdrill";
  setStatus("connect with a LeetDrill code", "bad");
}

async function showToday() {
  $("connectPanel").style.display = "none";
  $("todayPanel").style.display = "block";
  setStatus("loading Today...", "");
  const res = await send("LEETDRILL_TODAY_PROBLEMS");
  if (!res.ok) {
    setStatus(res.error || "could not load Today", "bad");
    return;
  }
  const data = res.data || {};
  const problems = data.problems || [];
  $("summary").textContent = `${data.completed_count || 0}/${data.total_count || problems.length} done`;
  const list = $("problems");
  list.textContent = "";
  if (!problems.length) {
    const empty = document.createElement("div");
    empty.className = "muted";
    empty.textContent = "No Today problems.";
    list.appendChild(empty);
  }
  for (const problem of problems) {
    const btn = document.createElement("button");
    btn.className = "problem" + (problem.completed ? " done" : "");
    btn.dataset.url = problem.url || `https://leetcode.com/problems/${problem.slug}/`;
    const title = document.createElement("span");
    title.className = "problem-title";
    title.textContent = `${problem.leetcode_id ? `${problem.leetcode_id}. ` : ""}${problem.title || problem.slug}`;
    const meta = document.createElement("span");
    meta.className = "problem-meta";
    meta.textContent = `${problem.difficulty || ""}${problem.completed ? " - done" : ""}`;
    btn.appendChild(title);
    btn.appendChild(meta);
    btn.addEventListener("click", () => {
      if (btn.dataset.url) ldx.tabs.create({ url: btn.dataset.url });
    });
    list.appendChild(btn);
  }
  setStatus("connected", "ok");
}

async function refresh() {
  const cfg = await send("LEETDRILL_GET_CONFIG");
  if (!cfg.ok) {
    setStatus("error reading config", "bad");
    return;
  }
  if (!cfg.data.token) {
    await showConnectPanel(cfg.data);
    return;
  }
  await showToday();
}

$("codePage").addEventListener("click", async () => {
  await saveBackend();
  const res = await send("LEETDRILL_OPEN_CODE_PAGE");
  setStatus(res.ok ? "opened login code page" : `open failed: ${res.error || "unknown error"}`, res.ok ? "ok" : "bad");
});

$("testConnection").addEventListener("click", async () => {
  await saveBackend();
  const res = await send("LEETDRILL_TEST_CONNECTION");
  if (!res.ok) {
    setStatus(`test failed: ${res.error || "unknown error"}`, "bad");
    return;
  }
  const data = res.data || {};
  if (data.connected) {
    setStatus("connection works", "ok");
  } else if (data.permission === "blocked") {
    setStatus(`browser blocked abhiy.xyz: ${data.message || "fetch failed"}`, "bad");
  } else {
    setStatus(data.message || "code missing or rejected", "bad");
  }
});

$("manualToken").addEventListener("input", scheduleTokenSave);
$("manualToken").addEventListener("paste", scheduleTokenSave);

refresh();
