#!/usr/bin/env python3
import argparse
import html
import json
import shutil
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
EXT = ROOT / "extension"
DIST = ROOT / "dist" / "extension-share"
SHARE_URL = "https://abhiyadav.in/shared/leetdrill-extension/"

SHARED_FILES = [
    "background.js",
    "compat.js",
    "content.js",
    "inject.js",
    "options.html",
    "options.js",
    "popup.html",
    "popup.js",
]

ICON_FILES = [
    "icons/icon16.png",
    "icons/icon48.png",
    "icons/icon128.png",
]


def load_json(path):
    with path.open() as f:
        return json.load(f)


def write_zip(out_path, base_dir, files):
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(out_path, "w", compression=zipfile.ZIP_DEFLATED) as zf:
        for rel in files:
            zf.write(base_dir / rel, rel)


def size_label(path):
    size = path.stat().st_size
    if size < 1024:
        return f"{size} B"
    return f"{size / 1024:.1f} KB"


def render_index(chrome_zip, firefox_zip, firefox_xpi, version):
    rows = [
        ("Chrome / Edge store package", chrome_zip.name, size_label(chrome_zip)),
        ("Firefox signing XPI", firefox_xpi.name, size_label(firefox_xpi)),
        ("Firefox source ZIP", firefox_zip.name, size_label(firefox_zip)),
    ]
    links = "\n".join(
        f"<li><a href=\"{html.escape(name)}\">{html.escape(label)}</a> "
        f"<span>{html.escape(size)}</span></li>"
        for label, name, size in rows
    )
    return f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>LeetDrill Extension</title>
  <style>
    body {{
      color: #172033;
      font: 16px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      margin: 0;
      background: #f7f8fb;
    }}
    main {{
      max-width: 760px;
      margin: 0 auto;
      padding: 48px 20px;
    }}
    h1 {{
      font-size: 34px;
      line-height: 1.15;
      margin: 0 0 12px;
    }}
    h2 {{
      font-size: 20px;
      margin: 32px 0 8px;
    }}
    p, li {{
      color: #3d4758;
    }}
    ul {{
      padding-left: 22px;
    }}
    .downloads {{
      list-style: none;
      padding: 0;
      margin: 24px 0;
      border: 1px solid #dde2eb;
      border-radius: 8px;
      overflow: hidden;
      background: white;
    }}
    .downloads li {{
      display: flex;
      justify-content: space-between;
      gap: 18px;
      padding: 14px 16px;
      border-top: 1px solid #edf0f5;
    }}
    .downloads li:first-child {{
      border-top: 0;
    }}
    a {{
      color: #0f5bd7;
      font-weight: 600;
      text-decoration: none;
    }}
    a:hover {{
      text-decoration: underline;
    }}
    code {{
      background: #edf0f5;
      border-radius: 4px;
      padding: 1px 5px;
    }}
    .note {{
      background: #fff8dc;
      border: 1px solid #ead98f;
      border-radius: 8px;
      padding: 14px 16px;
    }}
  </style>
</head>
<body>
  <main>
    <h1>LeetDrill Extension</h1>
    <p>Version {html.escape(version)} packages for the LeetDrill companion extension.</p>
    <p>LeetDrill is a practice tracker at <a href="https://abhiyadav.in/leetdrill">abhiyadav.in/leetdrill</a>.
      This extension captures LeetCode submission results, syncs LeetCode cookies
      only to the LeetDrill backend, and lets the backend import your solved
      history for daily practice planning.</p>

    <ul class="downloads">
      {links}
    </ul>

    <div class="note">
      Browser security rules limit direct installs from this page. Submit the Chrome
      package to the Chrome Web Store and the Firefox XPI/source package to AMO for
      signing before public installation.
    </div>

    <h2>Chrome / Edge</h2>
    <ol>
      <li>Submit <code>{html.escape(chrome_zip.name)}</code> in the Chrome Web Store dashboard.</li>
      <li>Use the permission and privacy text from <code>extension/STORE_LISTING.md</code>.</li>
    </ol>

    <h2>Firefox</h2>
    <ol>
      <li>Submit <code>{html.escape(firefox_xpi.name)}</code> to addons.mozilla.org for signing.</li>
      <li>Attach <code>{html.escape(firefox_zip.name)}</code> as source if requested during review.</li>
    </ol>

    <p>After install, sign in at <code>https://abhiyadav.in/leetdrill</code>. The extension
      can connect using that existing browser login.</p>
  </main>
</body>
</html>
"""


def main():
    parser = argparse.ArgumentParser(description="Build shareable LeetDrill extension packages.")
    parser.add_argument("--out", type=Path, default=DIST, help="output directory")
    args = parser.parse_args()

    out = args.out
    if out.exists():
        shutil.rmtree(out)
    out.mkdir(parents=True)

    chrome_manifest = load_json(EXT / "manifest.json")
    firefox_manifest = load_json(EXT / "firefox" / "manifest.json")
    version = chrome_manifest["version"]
    if firefox_manifest["version"] != version:
        raise SystemExit("chrome/firefox manifest versions differ")

    chrome_files = ["manifest.json", *SHARED_FILES, *ICON_FILES]
    firefox_files = ["manifest.json", *SHARED_FILES, *ICON_FILES]

    chrome_zip = out / f"leetdrill-chrome-{version}.zip"
    firefox_zip = out / f"leetdrill-firefox-{version}.zip"
    firefox_xpi = out / f"leetdrill-firefox-{version}.xpi"

    write_zip(chrome_zip, EXT, chrome_files)
    write_zip(firefox_zip, EXT / "firefox", firefox_files)
    write_zip(firefox_xpi, EXT / "firefox", firefox_files)
    (out / "index.html").write_text(render_index(chrome_zip, firefox_zip, firefox_xpi, version))

    print(SHARE_URL)
    for path in [chrome_zip, firefox_zip, firefox_xpi, out / "index.html"]:
        print(path)


if __name__ == "__main__":
    main()
