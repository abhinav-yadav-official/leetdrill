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

    require(chrome["manifest_version"] == 3, "chrome manifest must stay MV3")
    require(chrome["background"]["service_worker"] == "background.js", "chrome must use service worker")
    require((EXT / "compat.js").exists(), "shared compat.js missing")

    require(firefox["manifest_version"] == 2, "firefox manifest must use MV2")
    require("service_worker" not in firefox.get("background", {}), "firefox must not use background.service_worker")
    require(firefox["background"]["scripts"][:2] == ["compat.js", "background.js"], "firefox must load compat/background scripts")
    require(firefox["browser_action"]["default_popup"] == "popup.html", "firefox popup must use popup")
    require(firefox["options_ui"]["page"] == "options.html", "firefox options must use options")
    require("https://abhiy.xyz/*" in firefox["permissions"], "firefox permissions must include production backend")
    require("https://abhiy.xyz/*" in chrome["host_permissions"], "chrome host permissions must include production backend")

    for script in ["background.js", "content.js", "popup.js", "options.js"]:
        text = (EXT / script).read_text()
        require("ldx." in text, f"{script} must use extension compat wrapper")
        require((EXT / "firefox" / script).read_text() == text, f"firefox {script} must mirror shared {script}")

    for name in ["compat.js", "inject.js", "popup.html", "options.html"]:
        require((EXT / "firefox" / name).read_text() == (EXT / name).read_text(), f"firefox {name} must mirror shared {name}")


if __name__ == "__main__":
    main()
