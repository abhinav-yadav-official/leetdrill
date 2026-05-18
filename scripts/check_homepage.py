#!/usr/bin/env python3
from collections import defaultdict
from html.parser import HTMLParser
from pathlib import Path
import re
import time
from urllib.request import Request, build_opener, HTTPRedirectHandler


ROOT = Path(__file__).resolve().parents[1]
PAGE = ROOT / "web" / "homepage" / "index.html"
NOT_FOUND = ROOT / "web" / "homepage" / "404.html"
FORBIDDEN = ROOT / "web" / "homepage" / "403.html"
SERVER_ERROR = ROOT / "web" / "homepage" / "50x.html"
ADS = ROOT / "web" / "homepage" / "ads.txt"
RESUME = ROOT / "web" / "homepage" / "Abhinav_Resume.pdf"
DEPLOY = ROOT / "scripts" / "deploy_server.sh"
REPORT_KEYWORDS = ["backend", "production", "celery", "mysql", "redis"]
SEO_SEARCH_PHRASES = [
    "abhinav yadav",
    "instahyre",
    "fullstack developer",
]
EMAIL_RE = re.compile(r"[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}")


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def require_after(body, needle, anchor, message):
    anchor_index = body.find(anchor)
    needle_index = body.find(needle)
    require(anchor_index != -1, f"homepage must include {anchor}")
    require(needle_index != -1, f"homepage must include {needle}")
    require(needle_index > anchor_index, message)


class AnchorParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self._anchor_stack = []
        self.anchors = []

    def handle_starttag(self, tag, attrs):
        if tag == "a":
            self._anchor_stack.append({"href": dict(attrs).get("href", ""), "text": []})

    def handle_data(self, data):
        if self._anchor_stack:
            self._anchor_stack[-1]["text"].append(data)

    def handle_endtag(self, tag):
        if tag == "a" and self._anchor_stack:
            anchor = self._anchor_stack.pop()
            text = " ".join(" ".join(anchor["text"]).split())
            self.anchors.append((anchor["href"], text))


class SeoParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.title = []
        self.meta_description = ""
        self.heading_stack = []
        self.headings = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "meta" and attrs.get("name") == "description":
            self.meta_description = attrs.get("content", "")
        if tag in {"h1", "h2", "h3", "h4", "h5", "h6"}:
            self.heading_stack.append([])
        if tag == "title":
            self._in_title = True

    def handle_data(self, data):
        if getattr(self, "_in_title", False):
            self.title.append(data)
        if self.heading_stack:
            self.heading_stack[-1].append(data)

    def handle_endtag(self, tag):
        if tag == "title":
            self._in_title = False
        if tag in {"h1", "h2", "h3", "h4", "h5", "h6"} and self.heading_stack:
            heading = " ".join(" ".join(self.heading_stack.pop()).split())
            self.headings.append(heading)


def require_unique_anchor_text(body):
    parser = AnchorParser()
    parser.feed(body)
    by_text = defaultdict(list)
    for href, text in parser.anchors:
        if text:
            by_text[text].append(href)
    duplicates = {
        text: hrefs
        for text, hrefs in by_text.items()
        if len(hrefs) > 1
    }
    require(not duplicates, f"homepage anchor text must be unique: {duplicates}")


def require_report_keyword_coverage(body):
    parser = SeoParser()
    parser.feed(body)
    title = " ".join(" ".join(parser.title).split()).lower()
    meta = parser.meta_description.lower()
    headings = " ".join(parser.headings).lower()
    require(150 <= len(parser.meta_description) <= 220, "meta description must be 150-220 chars")
    for phrase in ["abhinav yadav", "instahyre", "fullstack", "backend"]:
        require(phrase in title, f"title must include primary SEO phrase: {phrase}")
    for keyword in REPORT_KEYWORDS:
        require(keyword in meta, f"meta description must include SEO keyword: {keyword}")
        require(keyword in headings, f"heading tags must include SEO keyword: {keyword}")
    combined = " ".join([title, meta, headings])
    for phrase in SEO_SEARCH_PHRASES:
        require(phrase in combined, f"homepage SEO tags must include search phrase: {phrase}")


def require_no_plaintext_email(body):
    matches = EMAIL_RE.findall(body)
    require(not matches, f"homepage must not expose plaintext emails: {matches}")


class NoRedirectHandler(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def require_redirect(url, location):
    opener = build_opener(NoRedirectHandler)
    request = Request(url, method="HEAD")
    last_error = None
    for _ in range(3):
        try:
            opener.open(request, timeout=10)
        except Exception as exc:
            response = getattr(exc, "headers", {})
            code = getattr(exc, "code", None)
            actual_location = response.get("Location")
            if code in (301, 308) and actual_location == location:
                return
            last_error = f"{url} returned {code} with Location {actual_location}"
            time.sleep(0.5)
            continue
        last_error = f"{url} did not redirect"
        time.sleep(0.5)
    require(False, last_error or f"{url} must redirect to {location}")


def main():
    body = PAGE.read_text()
    deploy = DEPLOY.read_text()
    require_unique_anchor_text(body)
    require_report_keyword_coverage(body)
    require_no_plaintext_email(body)
    require(NOT_FOUND.exists(), "homepage must include custom 404.html")
    require("Page not found" in NOT_FOUND.read_text(), "custom 404 page must explain missing page")
    require(FORBIDDEN.exists(), "homepage must include custom 403.html")
    require("Access restricted" in FORBIDDEN.read_text(), "custom 403 page must explain restricted access")
    require(SERVER_ERROR.exists(), "homepage must include custom 50x.html")
    require("Service unavailable" in SERVER_ERROR.read_text(), "custom 50x page must explain server errors")
    require(ADS.exists(), "homepage must include ads.txt")
    require(RESUME.exists(), "homepage resume PDF must exist")
    require(RESUME.stat().st_size > 0, "homepage resume PDF must not be empty")
    require('href="/resume"' in body, "homepage must link resume via /resume")
    require("Abhinav" in body, "homepage must mention Abhinav")
    require("Abhinav Yadav" in body, "homepage must include full name for search")
    require("SDE III" in body, "homepage must mention current role")
    require('data-email-local="me"' in body, "homepage must obfuscate contact email local part")
    require('data-email-domain="abhiyadav.in"' in body, "homepage must obfuscate contact email domain")
    require('href="/linkedin"' in body, "homepage must link LinkedIn via /linkedin")
    require('href="/github"' in body, "homepage must link GitHub profile via /github")
    require(
        "https://leetcode.com/u/abhinav-yadav-official/" in body,
        "homepage must link LeetCode profile",
    )
    require("https://github.com/abhinav-yadav-official/leetdrill" in body, "homepage must link GitHub repo")
    require("https://abhiyadav.in/leetdrill" in body, "homepage must link hosted LeetDrill")
    require("LeetDrill" in body, "homepage must mention LeetDrill")
    require("devportfolio-inspired" in body, "homepage must identify devportfolio-inspired redesign")
    require("color-scheme: dark" in body, "homepage must use dark color scheme")
    require("IBM Plex Mono" in body, "homepage must use IBM Plex Mono")
    require("Hello!" in body, "homepage must use devportfolio-style greeting")
    require("typing-line" in body, "homepage must mark hero typing lines")
    require("typing-cursor" in body, "homepage must include typing cursor")
    require("typeHeroLine" in body, "homepage must include hero typing script")
    require("prefers-reduced-motion: reduce" in body, "homepage typing must respect reduced motion")
    require("Instahyre backend and fullstack work" in body, "homepage must include Instahyre fullstack section")
    require("Education" in body, "homepage must include Education section")
    require("senior backend engineer" in body.lower(), "homepage must include backend about summary")
    require("backdrop-filter: blur(4.5px)" in body, "homepage desktop navbar must use lighter blur")
    require("-webkit-backdrop-filter: blur(4.5px)" in body, "homepage desktop navbar must use lighter safari blur")
    require("linear-gradient(to bottom" in body, "homepage navbar background must fade out at bottom")
    require("border-bottom: 1px solid rgba(148, 163, 184, 0.18)" not in body, "homepage desktop navbar must not show a border line")
    require(".site-nav {\n          display: none;" in body, "homepage mobile navbar must be hidden")
    require("scroll-padding-top" in body, "homepage anchors must account for fixed navbar")
    require(".hero-actions {\n          display: flex;" in body, "homepage mobile links must wrap instead of stacking vertically")
    require("#60a5fa" in body, "homepage must define accessible blue accent color")
    require("#020617" in body, "homepage must define a dark page background")
    require("--ink: #f8fafc" in body, "homepage must define high-contrast foreground text")
    require("--muted: #cbd5e1" in body, "homepage must define readable muted text")
    require("programming-symbols" in body, "homepage must include code symbol hero background")
    require("@media (max-width: 620px)" in body, "homepage must include mobile breakpoint")
    require("@media (max-width: 420px)" in body, "homepage must include narrow mobile breakpoint")
    require("overflow-wrap: anywhere" in body, "homepage must prevent mobile text overflow")
    require("@keyframes" in body, "homepage must define CSS animations")
    require("animation:" in body, "homepage must apply CSS animations")
    require("transition:" in body, "homepage must include interactive transitions")
    require("prefers-reduced-motion" in body, "homepage must respect reduced motion")
    for repo in [
        "manager.ai",
        "legacy-mac-wheels",
        "homebrew-legacy",
        "dotfiles",
        "register-page",
        "object-detect",
        "doc-scan",
        "mern-user-authentication",
        "angular-freehand-drawing-app",
        "keras-flask-app",
        "react-clock-app",
    ]:
        require(
            f"https://github.com/abhinav-yadav-official/{repo}" in body,
            f"homepage must link GitHub repo {repo}",
        )
    for repo in [
        "manager.ai",
        "legacy-mac-wheels",
        "homebrew-legacy",
        "dotfiles",
        "register-page",
        "object-detect",
        "doc-scan",
        "mern-user-authentication",
        "angular-freehand-drawing-app",
        "keras-flask-app",
        "react-clock-app",
    ]:
        require_after(
            body,
            f"https://github.com/abhinav-yadav-official/{repo}",
            "Project archive",
            f"{repo} must appear in archive section",
        )
    require("Production systems: Redis, Celery, MySQL" in body, "homepage must include systems section")
    require(
        "--exclude=shared/" in deploy,
        "homepage deploy must preserve /var/www/html/shared extension downloads",
    )
    require("error_page 403 /403.html;" in deploy, "nginx deploy must use custom 403 page")
    require("error_page 404 /404.html;" in deploy, "nginx deploy must use custom 404 page")
    require("error_page 500 502 503 504 /50x.html;" in deploy, "nginx deploy must use custom 50x page")
    require("proxy_intercept_errors on;" in deploy, "nginx deploy must intercept upstream error pages")
    require("open_file_cache max=1000 inactive=60s;" in deploy, "nginx deploy must enable open file cache")
    require("gzip_static on;" in deploy, "nginx deploy must enable static gzip")
    require("stale-while-revalidate=86400" in deploy, "homepage deploy must cache homepage briefly")
    for url in [
        "http://abhiy.xyz/",
        "https://abhiy.xyz/",
        "http://www.abhiy.xyz/",
        "https://www.abhiy.xyz/",
    ]:
        require_redirect(url, "https://abhiyadav.in/")
    require_redirect(
        "https://abhiy.xyz/old-path?source=seo",
        "https://abhiyadav.in/old-path?source=seo",
    )


if __name__ == "__main__":
    main()
