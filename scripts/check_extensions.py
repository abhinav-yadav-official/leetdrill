#!/usr/bin/env python3
import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
EXT = ROOT / "extension"


def load_json(path):
    with path.open() as f:
        return json.load(f)


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def main():
    chrome = load_json(EXT / "manifest.json")
    firefox = load_json(EXT / "firefox" / "manifest.json")
    description = chrome.get("description", "")

    require(chrome["manifest_version"] == 3, "chrome manifest must stay MV3")
    require(chrome["background"]["service_worker"] == "background.js", "chrome must use service worker")
    require(chrome["background"].get("type") != "module", "chrome background must stay classic for importScripts")
    require((EXT / "compat.js").exists(), "shared compat.js missing")
    require("abhiy.xyz/leetdrill" in description, "description must identify the LeetDrill web app")
    require("Spaced" not in description and "SRS" not in description, "description must avoid jargon")

    require(firefox["manifest_version"] == 2, "firefox manifest must use MV2")
    require("service_worker" not in firefox.get("background", {}), "firefox must not use background.service_worker")
    require(firefox["background"]["scripts"][:2] == ["compat.js", "background.js"], "firefox must load compat/background scripts")
    require(firefox["browser_action"]["default_popup"] == "popup.html", "firefox popup must use popup")
    require(firefox["options_ui"]["page"] == "options.html", "firefox options must use options")
    require("https://abhiy.xyz/*" in firefox["permissions"], "firefox permissions must include production backend")
    require("https://abhiy.xyz/*" in chrome["host_permissions"], "chrome host permissions must include production backend")
    for host in ["http://localhost/*", "http://127.0.0.1/*"]:
        require(host not in chrome["host_permissions"], f"chrome store package must not request {host}")
        require(host not in firefox["permissions"], f"firefox store package must not request {host}")
    require("tabs" not in firefox["permissions"], "firefox must not request tabs permission for tabs.create")
    require(chrome["icons"]["128"] == "icons/icon128.png", "chrome must include icons")
    require(firefox["icons"]["128"] == "icons/icon128.png", "firefox must include icons")
    require((EXT / "STORE_LISTING.md").exists(), "store listing notes missing")
    require((EXT / "PRIVACY.md").exists(), "privacy notes missing")

    for script in ["background.js", "content.js", "popup.js"]:
        text = (EXT / script).read_text()
        require("ldx." in text, f"{script} must use extension compat wrapper")
        require((EXT / "firefox" / script).read_text() == text, f"firefox {script} must mirror shared {script}")
    require("ldx." in (EXT / "options.js").read_text(), "options.js must use extension compat wrapper")
    firefox_options = (EXT / "firefox" / "options.js").read_text()
    require("ldx." in firefox_options, "firefox options.js must use extension compat wrapper")

    compat = (EXT / "compat.js").read_text()
    background = (EXT / "background.js").read_text()
    options = (EXT / "options.js").read_text()
    require("getAll: wrap(api.cookies.getAll" in compat, "compat must expose cookies.getAll for login diagnostics")
    require("LEETDRILL_CONNECT_STATUS" in background, "background must expose connection status diagnostics")
    require("findBackendSessionCookie" in background, "background must search backend login cookies robustly")
    require("LEETDRILL_OPEN_WEB_CONNECT" in background, "background must support first-party web connect")
    require("LEETDRILL_SAVE_TOKEN" in background, "background must support manual token fallback")
    require("LEETDRILL_OPEN_APP" in background, "background must open LeetDrill through tabs API")
    require("LEETDRILL_TEST_CONNECTION" in background, "background must expose backend permission/auth test")
    require("sender.tab.url" in background, "background must trust Firefox content-script sender tab URLs")
    require("LEETDRILL_SAVE_TOKEN" in options, "options must expose manual token fallback")
    require("LEETDRILL_OPEN_APP" not in options, "options must not show open LeetDrill link")
    require("LEETDRILL_TEST_CONNECTION" in options, "options must verify saved login tokens automatically")
    require("LEETDRILL_OPEN_WEB_CONNECT" not in options, "options must not use browser-login connect")
    require("LEETDRILL_HANDSHAKE" not in options, "options must not expose login/password connect")
    require("LEETDRILL_OPEN_CODE_PAGE" in options, "options must expose code page link")
    require("LEETDRILL_OPEN_WEB_CONNECT" not in firefox_options, "firefox options must not use browser-login connect")
    require("LEETDRILL_HANDSHAKE" not in firefox_options, "firefox options must not expose login/password connect")
    require("LEETDRILL_OPEN_CODE_PAGE" in firefox_options, "firefox options must expose code page link")
    require("LEETDRILL_SAVE_TOKEN" in firefox_options, "firefox options must keep manual code fallback")
    content = (EXT / "content.js").read_text()
    require("LEETDRILL_EXTENSION_TOKEN" in content, "content script must accept web connect token")
    require("LEETDRILL_WEB_CONNECT_TOKEN" in content, "content script must listen for web connect broadcasts")
    require("https://abhiy.xyz/leetdrill/extension/connect*" in str(chrome.get("content_scripts", [])), "chrome must inject on web connect page")
    require("https://abhiy.xyz/leetdrill/extension/connect*" in str(firefox.get("content_scripts", [])), "firefox must inject on web connect page")
    popup_html = (EXT / "popup.html").read_text()
    popup_js = (EXT / "popup.js").read_text()
    require("sync cookies" not in popup_html and "import history" not in popup_html, "popup must not show manual sync/import buttons")
    require("LEETDRILL_TODAY_PROBLEMS" in background and "LEETDRILL_TODAY_PROBLEMS" in popup_js, "popup must load all Today problems")
    require("LEETDRILL_OPEN_CODE_PAGE" in popup_js and "LEETDRILL_SAVE_TOKEN" in popup_js, "popup must expose code connect when disconnected")
    require("saveToken" not in popup_html and "save manual code" not in popup_html, "popup must auto-save login tokens without a save button")
    require("saveToken" not in (EXT / "options.html").read_text() and "save manual code" not in (EXT / "options.html").read_text(), "options must auto-save login tokens without a save button")
    require("testConnection" not in popup_html and "testConnection" not in (EXT / "options.html").read_text(), "popup/options must not show test buttons")
    require("Click here to obtain login code" in popup_html and "Click here to obtain login code" in (EXT / "options.html").read_text(), "connect button text must point to login code")
    require("Login Token" in popup_html and "Login Token" in (EXT / "options.html").read_text(), "manual code label must be Login Token")
    require("scheduleTokenSave" in popup_js and "scheduleTokenSave" in options, "popup/options must auto-save pasted login tokens")

    for name in ["compat.js", "inject.js", "popup.html", "options.html"]:
        require((EXT / "firefox" / name).read_text() == (EXT / name).read_text(), f"firefox {name} must mirror shared {name}")
    require((EXT / "firefox" / "options.js").read_text() == (EXT / "options.js").read_text(), "firefox options.js must mirror shared options.js")
    require("use browser login" not in (EXT / "firefox" / "options.html").read_text(), "firefox options must not show browser-login UI")
    require("Login Token" in (EXT / "firefox" / "options.html").read_text(), "firefox options must show login token UI")

    for icon in ["icon16.png", "icon48.png", "icon128.png"]:
        require((EXT / "icons" / icon).exists(), f"chrome icon missing: {icon}")
        require((EXT / "firefox" / "icons" / icon).exists(), f"firefox icon missing: {icon}")


if __name__ == "__main__":
    main()
