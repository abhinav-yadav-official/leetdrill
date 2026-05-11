// Cross-browser WebExtension helpers. Chrome exposes `chrome`; Firefox exposes
// `browser` with Promise-returning APIs. Keep extension code on one surface.
(function () {
  const api = globalThis.browser || globalThis.chrome;
  const promiseApi = Boolean(globalThis.browser);

  function wrap(fn, receiver) {
    return (...args) =>
      new Promise((resolve, reject) => {
        if (promiseApi) {
          try {
            Promise.resolve(fn.call(receiver, ...args)).then(resolve, reject);
          } catch (err) {
            reject(err);
          }
          return;
        }
        let settled = false;
        const done = (value) => {
          if (!settled) {
            settled = true;
            resolve(value);
          }
        };
        try {
          const ret = fn.call(receiver, ...args, (value) => {
            const err = globalThis.chrome && chrome.runtime && chrome.runtime.lastError;
            if (err && !settled) {
              settled = true;
              reject(new Error(err.message));
              return;
            }
            done(value);
          });
          if (ret && typeof ret.then === "function") {
            ret.then(done, reject);
          } else if (ret !== undefined) {
            done(ret);
          }
        } catch (err) {
          reject(err);
        }
      });
  }

  function toErrorResponse(err) {
    return { ok: false, error: err.message || String(err) };
  }

  function addMessageListener(handler) {
    api.runtime.onMessage.addListener((message, sender, sendResponse) => {
      try {
        const result = handler(message, sender);
        if (promiseApi) {
          return result;
        }
        if (result && typeof result.then === "function") {
          result.then(sendResponse, (err) => sendResponse(toErrorResponse(err)));
          return true;
        }
        sendResponse(result);
      } catch (err) {
        if (!promiseApi) {
          sendResponse(toErrorResponse(err));
        }
        return promiseApi ? Promise.resolve(toErrorResponse(err)) : false;
      }
      return false;
    });
  }

  const action = api.action || api.browserAction;

  globalThis.ldx = {
    storage: {
      get: wrap(api.storage.local.get, api.storage.local),
      set: wrap(api.storage.local.set, api.storage.local)
    },
    cookies: {
      get: wrap(api.cookies.get, api.cookies)
    },
    alarms: {
      create: wrap(api.alarms.create, api.alarms),
      onAlarm: api.alarms.onAlarm
    },
    action: {
      setBadgeText: wrap(action.setBadgeText, action),
      setBadgeBackgroundColor: wrap(action.setBadgeBackgroundColor, action)
    },
    runtime: {
      getURL: (path) => api.runtime.getURL(path),
      lastError: () => api.runtime.lastError,
      onInstalled: api.runtime.onInstalled,
      onMessage: { addListener: addMessageListener },
      openOptionsPage: wrap(api.runtime.openOptionsPage, api.runtime),
      sendMessage: wrap(api.runtime.sendMessage, api.runtime)
    },
    tabs: {
      create: wrap(api.tabs.create, api.tabs)
    }
  };
})();
