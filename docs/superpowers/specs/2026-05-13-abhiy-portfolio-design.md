# Abhiy Portfolio Homepage Design

## Goal

Replace the current minimal `abhiy.xyz` homepage with a polished editorial portfolio for Abhinav Yadav that highlights backend engineering experience, selected projects, public links, and a downloadable resume.

## Audience

The page is for recruiters, engineering peers, and people who land on `abhiy.xyz` from project links. It should communicate senior backend engineering credibility quickly without becoming a marketing page.

## Content

- Use `Abhinav Yadav` as the main first-viewport signal.
- Show current role as `SDE III at Instahyre`.
- Include public contact links from the resume: email, LinkedIn, GitHub.
- Include a direct resume link to `Abhinav_Resume.pdf`.
- Feature LeetDrill with links to `https://abhiy.xyz/leetdrill` and `https://github.com/abhinav-yadav-official/leetdrill`.
- Summarize experience around backend systems, async processing, search, caching, migrations, performance, and production reliability.
- Include a compact skills section covering Python, Go, Django, Celery, RabbitMQ, MySQL, Redis, Elasticsearch/OpenSearch, AWS, Datadog, CloudWatch, and Sentry.

## Visual Direction

Use the approved editorial profile direction:

- Light, restrained page with strong typography and generous whitespace.
- Warm neutral background with crisp dark text and restrained accent color.
- First screen should feel like a personal profile, not a product dashboard.
- Use structured sections and individual project/experience cards only where they improve scanning.
- Keep the page static and fast, with all CSS in `web/homepage/index.html`.

## Implementation Scope

- Modify `web/homepage/index.html`.
- Add `web/homepage/Abhinav_Resume.pdf` copied from `~/Abhinav_Resume.pdf`.
- Update `scripts/check_homepage.py` so the static checks protect the new portfolio content and resume asset.
- Update `scripts/deploy_server.sh` so homepage assets beyond `index.html` are published to `/var/www/html/`.

## Verification

- Run `python3 scripts/check_homepage.py`.
- Run `go test ./...` to make sure unrelated app tests still pass.
- Optionally serve `web/homepage` locally and inspect responsive layout in a browser.
