#!/usr/bin/env python3
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PAGE = ROOT / "web" / "homepage" / "index.html"
RESUME = ROOT / "web" / "homepage" / "Abhinav_Resume.pdf"
DEPLOY = ROOT / "scripts" / "deploy_server.sh"


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def require_after(body, needle, anchor, message):
    anchor_index = body.find(anchor)
    needle_index = body.find(needle)
    require(anchor_index != -1, f"homepage must include {anchor}")
    require(needle_index != -1, f"homepage must include {needle}")
    require(needle_index > anchor_index, message)


def main():
    body = PAGE.read_text()
    deploy = DEPLOY.read_text()
    require(RESUME.exists(), "homepage resume PDF must exist")
    require(RESUME.stat().st_size > 0, "homepage resume PDF must not be empty")
    require('href="/resume"' in body, "homepage must link resume via /resume")
    require("Abhinav" in body, "homepage must mention Abhinav")
    require("Abhinav Yadav" not in body, "homepage must use Abhinav instead of Abhinav Yadav")
    require("SDE III" in body, "homepage must mention current role")
    require("abhi.ay.in@gmail.com" in body, "homepage must link email")
    require('href="/linkedin"' in body, "homepage must link LinkedIn via /linkedin")
    require('href="/github"' in body, "homepage must link GitHub profile via /github")
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
    require("About Me" in body, "homepage must include About Me section")
    require("Education" in body, "homepage must include Education section")
    require("Senior backend engineer" in body, "homepage must include backend about summary")
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
    require("Systems I work on" in body, "homepage must include systems section")
    require(
        "--exclude=shared/" in deploy,
        "homepage deploy must preserve /var/www/html/shared extension downloads",
    )


if __name__ == "__main__":
    main()
