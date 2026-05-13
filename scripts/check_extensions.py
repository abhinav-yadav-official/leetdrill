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

    for script in ["background.js", "content.js", "popup.js", "options.js"]:
        text = (EXT / script).read_text()
        require("ldx." in text, f"{script} must use extension compat wrapper")
        require((EXT / "firefox" / script).read_text() == text, f"firefox {script} must mirror shared {script}")

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
    require("LEETDRILL_CONNECT_STATUS" in options, "options must show browser-login diagnostics")
    require("LEETDRILL_SAVE_TOKEN" in options, "options must expose manual token fallback")
    require("LEETDRILL_OPEN_APP" in options, "options must make LeetDrill link clickable")
    require("LEETDRILL_TEST_CONNECTION" in options, "options must expose backend permission/auth test")
    content = (EXT / "content.js").read_text()
    require("LEETDRILL_EXTENSION_TOKEN" in content, "content script must accept web connect token")
    require("LEETDRILL_WEB_CONNECT_TOKEN" in content, "content script must listen for web connect broadcasts")
    require("https://abhiy.xyz/leetdrill/extension/connect*" in str(chrome.get("content_scripts", [])), "chrome must inject on web connect page")
    require("https://abhiy.xyz/leetdrill/extension/connect*" in str(firefox.get("content_scripts", [])), "firefox must inject on web connect page")

    for name in ["compat.js", "inject.js", "popup.html", "options.html"]:
        require((EXT / "firefox" / name).read_text() == (EXT / name).read_text(), f"firefox {name} must mirror shared {name}")

    for icon in ["icon16.png", "icon48.png", "icon128.png"]:
        require((EXT / "icons" / icon).exists(), f"chrome icon missing: {icon}")
        require((EXT / "firefox" / "icons" / icon).exists(), f"firefox icon missing: {icon}")


if __name__ == "__main__":
    main()
