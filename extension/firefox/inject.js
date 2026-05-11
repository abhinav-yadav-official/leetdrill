// Runs in page context, so it can shadow window.fetch. Forwards verdict
// payloads back to content.js via window.postMessage.
//
// LeetCode polls /submissions/detail/<id>/check/ until `state === "SUCCESS"`.
// That response carries the final verdict, runtime, memory, code, and the
// submission id. Hooking fetch here is *much* more stable than parsing the
// verdict modal DOM, which they redesign occasionally.

(function () {
  if (window.__LEETDRILL_HOOKED__) return;
  window.__LEETDRILL_HOOKED__ = true;

  const origFetch = window.fetch;

  window.fetch = async function (...args) {
    const res = await origFetch.apply(this, args);
    try {
      const reqUrl = typeof args[0] === "string" ? args[0] : args[0].url;
      if (reqUrl && /\/submissions\/detail\/\d+\/check\/?$/.test(reqUrl)) {
        const clone = res.clone();
        clone
          .json()
          .then((data) => {
            if (!data || data.state !== "SUCCESS") return;
            const slugMatch = window.location.pathname.match(/^\/problems\/([^/]+)/);
            window.postMessage(
              {
                type: "LEETDRILL_SUBMISSION",
                payload: {
                  slug: slugMatch ? slugMatch[1] : "",
                  verdict: data.status_msg || "Unknown",
                  runtime_ms: parseInt(String(data.status_runtime || "").replace(/[^\d]/g, ""), 10) || null,
                  memory_kb: parseInt(String(data.status_memory || "").replace(/[^\d]/g, ""), 10) || null,
                  language: data.pretty_lang || data.lang || "",
                  code: data.code || "",
                  submission_id: data.submission_id || null,
                  total_correct: data.total_correct,
                  total_testcases: data.total_testcases
                }
              },
              "*"
            );
          })
          .catch(() => {});
      }
    } catch (e) {
      // Don't break the page on our account.
      console.warn("[leetdrill] fetch hook error:", e);
    }
    return res;
  };
})();
