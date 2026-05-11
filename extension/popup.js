// LeetDrill popup. Shows next-due problem, opens it on click.

function send(type, payload) {
  return new Promise((resolve) => {
    chrome.runtime.sendMessage({ type, payload }, (res) => resolve(res || { ok: false }));
  });
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
    status.textContent = "not connected — open options";
    status.classList.add("bad");
    return;
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
  if (url) chrome.tabs.create({ url });
});

$("sync").addEventListener("click", async () => {
  $("sync").disabled = true;
  const res = await send("LEETDRILL_SYNC_COOKIES");
  $("sync").disabled = false;
  $("sync").textContent = res.ok ? "synced ✓" : "sync failed";
  setTimeout(() => { $("sync").textContent = "sync cookies"; }, 1500);
});

$("options").addEventListener("click", () => {
  chrome.runtime.openOptionsPage();
});

refresh();
