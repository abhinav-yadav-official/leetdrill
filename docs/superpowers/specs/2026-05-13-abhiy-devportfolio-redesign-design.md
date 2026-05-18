# Abhiy Devportfolio-Style Redesign Design

## Goal

Redesign `abhiy.xyz` to use a light, minimalist developer portfolio style inspired by `RyanFitzgerald/devportfolio`, while keeping the implementation as a static homepage in this repository.

## UI Direction

- Use a light page with IBM Plex Mono and blue accent color.
- Use a full-height hero with:
  - `Hello!`
  - `I'm Abhinav Yadav`
  - `SDE III at Instahyre`
  - Resume, email, LinkedIn, GitHub, and LeetDrill links.
- Use subtle radial accent wash, code-symbol background, and grid pattern in the hero.
- Use centered fixed desktop navigation with a translucent background after scroll.
- Use section layout similar to the inspiration: large left heading and right content column.
- Use card-based Projects, Archive, Experience, and Education sections.
- Keep all content mobile-friendly and avoid horizontal overflow.

## Content

- About section summarizes backend systems work and uses skill pills.
- Projects section features LeetDrill only.
- Project archive contains the other public GitHub repositories.
- Experience section uses Instahyre SDE III, SDE II, and SDE I entries with concise bullets.
- Education section includes B.Tech Computer Science, AKTU.

## Implementation

- Modify `web/homepage/index.html`.
- Modify `scripts/check_homepage.py` to enforce the new light devportfolio-style markers and preserve required links.
- Keep `web/homepage/Abhinav_Resume.pdf` as the resume asset.

## Verification

- `python3 scripts/check_homepage.py`
- HTML parser sanity check.
- Playwright desktop and mobile screenshots.
- `go test ./...`
- Deploy to `abhiy.xyz` and verify live content, resume PDF, and LeetDrill health.
