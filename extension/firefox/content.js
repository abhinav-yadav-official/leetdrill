// LeetDrill content script. Runs in isolated world on leetcode.com/problems/*.
//
// Two jobs:
//   1. Inject inject.js into page context so it can shadow window.fetch and
//      capture submission-check responses (the polling endpoint).
//   2. Forward LEETDRILL_SUBMISSION postMessage events from page → background
//      enriched with slug + page-open timing.

(function () {
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
