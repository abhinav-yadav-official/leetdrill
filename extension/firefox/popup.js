// LeetDrill popup. Shows next-due problem, opens it on click.

function send(type, payload) {
  return ldx.runtime
    .sendMessage({ type, payload })
    .then((res) => res || { ok: false })
    .catch((err) => ({ ok: false, error: err.message || String(err) }));
}

const $ = (id) => document.getElementById(id);

async function refresh() {
  const cfg = await send("LEETDRILL_GET_CONFIG");
  const status = $("status");
  if (!cfg.ok) {
    status.textContent = "error reading config";
    status.classList.add("bad");
    return;
  }
  if (!cfg.data.token) {
    status.textContent = "checking LeetDrill login…";
    const connected = await send("LEETDRILL_ENSURE_CONNECTED");
    if (!connected.ok || !connected.data || !connected.data.token) {
      status.textContent = "sign in to abhiy.xyz/leetdrill, then reopen this popup";
      status.classList.add("bad");
      return;
    }
    cfg.data = connected.data;
  }
  status.textContent = `connected to ${cfg.data.backendUrl}`;
  status.classList.add("ok");

  const next = await send("LEETDRILL_NEXT_PROBLEM");
  if (!next.ok) {
    $("next").style.display = "none";
    $("open").disabled = true;
    $("open").textContent = next.error || "no problem available";
    return;
  }
  const np = next.data;
  $("nextTitle").textContent = np.title || np.slug;
  $("nextDifficulty").textContent = np.difficulty || "";
  const reason = $("nextReason");
  reason.textContent = np.reason || "";
  reason.className = "badge " + (np.reason || "");
  $("next").style.display = "block";
  $("open").disabled = false;
  $("open").dataset.url = np.url || `https://leetcode.com/problems/${np.slug}/`;
}

$("open").addEventListener("click", () => {
  const url = $("open").dataset.url;
  if (url) ldx.tabs.create({ url });
});

$("sync").addEventListener("click", async () => {
  $("sync").disabled = true;
  const res = await send("LEETDRILL_SYNC_COOKIES");
  $("sync").disabled = false;
  $("sync").textContent = res.ok ? "synced ✓" : "sync failed";
  setTimeout(() => { $("sync").textContent = "sync cookies"; }, 1500);
});

$("import").addEventListener("click", async () => {
  $("import").disabled = true;
  $("import").textContent = "importing…";
  const res = await send("LEETDRILL_COLD_START", {});
  $("import").disabled = false;
  if (res.ok) {
    const d = res.data || {};
    $("import").textContent = `imported ${((d.recent_imported || 0) + (d.authed_imported || 0))}`;
    refresh();
  } else {
    $("import").textContent = "import failed";
  }
  setTimeout(() => { $("import").textContent = "import history"; }, 2500);
});

$("options").addEventListener("click", () => {
  ldx.runtime.openOptionsPage();
});

refresh();
