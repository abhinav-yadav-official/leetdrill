// LeetDrill content script. Runs in isolated world on leetcode.com/problems/*.
//
// Two jobs:
//   1. Inject inject.js into page context so it can shadow window.fetch and
//      capture submission-check responses (the polling endpoint).
//   2. Forward LEETDRILL_SUBMISSION postMessage events from page → background
//      enriched with slug + page-open timing.

(function () {
  function sendConnectResult(error) {
    window.postMessage(
      { type: "LEETDRILL_WEB_CONNECT_DONE", error: error || "" },
      window.location.origin
    );
  }

  function saveExtensionToken(token) {
    const status = document.getElementById("leetdrill-extension-status");
    if (!token) {
      if (status) status.textContent = "Could not find the extension token. Sign in and try again.";
      sendConnectResult("Could not find the extension token. Sign in and try again.");
      return;
    }
    ldx.runtime
      .sendMessage({ type: "LEETDRILL_EXTENSION_TOKEN", payload: { token } })
      .then((res) => {
        if (status) {
          status.textContent = res && res.ok
            ? "Extension connected. You can close this tab."
            : `Extension connect failed: ${(res && res.error) || "unknown error"}`;
        }
        sendConnectResult(res && res.ok ? "" : `Extension connect failed: ${(res && res.error) || "unknown error"}`);
      })
      .catch((err) => {
        const msg = `Extension connect failed: ${err.message || String(err)}`;
        if (status) status.textContent = msg;
        sendConnectResult(msg);
      });
  }

  function handleExtensionConnectPage() {
    const meta = document.querySelector('meta[name="leetdrill-extension-token"]');
    saveExtensionToken(meta ? meta.content : "");
    window.addEventListener("message", (ev) => {
      if (ev.source !== window || ev.origin !== window.location.origin) return;
      const data = ev.data || {};
      if (data.type !== "LEETDRILL_WEB_CONNECT_TOKEN") return;
      saveExtensionToken(data.token || "");
    });
  }

  if (window.location.hostname === "abhiyadav.in" &&
      window.location.pathname === "/leetdrill/extension/connect") {
    handleExtensionConnectPage();
    return;
  }

  const PAGE_OPENED_AT = Date.now();

  function slugFromPath() {
    // /problems/two-sum/ → "two-sum"
    const m = window.location.pathname.match(/^\/problems\/([^/]+)/);
    return m ? m[1] : "";
  }

  // Inject the page-context script. We point at the bundled inject.js so it
  // shares CSP origin with the page rather than `chrome-extension:`.
  const s = document.createElement("script");
  s.src = ldx.runtime.getURL("inject.js");
  s.onload = () => s.remove();
  (document.head || document.documentElement).appendChild(s);

  // Submissions in a session: we want submission_count (1, 2, ...). The
  // simplest reliable counter is "times the verdict modal has fired" because
  // both AC and non-AC poll the same /check/ endpoint.
  let submissionsThisPage = 0;

  window.addEventListener("message", (ev) => {
    if (ev.source !== window) return;
    const data = ev.data;
    if (!data || data.type !== "LEETDRILL_SUBMISSION") return;

    submissionsThisPage += 1;
    const payload = {
      slug: data.payload.slug || slugFromPath(),
      verdict: data.payload.verdict || "Unknown",
      submission_count: submissionsThisPage,
      time_taken_sec: Math.floor((Date.now() - PAGE_OPENED_AT) / 1000),
      runtime_ms: data.payload.runtime_ms || null,
      memory_kb: data.payload.memory_kb || null,
      language: data.payload.language || "",
      code: data.payload.code || "",
      leetcode_submission_id: data.payload.submission_id
        ? String(data.payload.submission_id)
        : "",
      started_at_unix: Math.floor(PAGE_OPENED_AT / 1000),
      completed_at_unix: Math.floor(Date.now() / 1000)
    };

    ldx.runtime
      .sendMessage({ type: "LEETDRILL_SUBMISSION", payload })
      .then((res) => {
        if (res && res.ok) {
          console.log("[leetdrill] submission applied:", res.data);
        } else {
          console.warn("[leetdrill] submission rejected:", res && res.error);
        }
      })
      .catch((err) => {
        console.warn("[leetdrill] sendMessage:", err.message || String(err));
      });
  });
})();
