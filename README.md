# leetdrill

LeetDrill is a self-hosted LeetCode practice tracker. It imports the LeetCode
catalog, captures your accepted submissions through a browser extension, and
keeps a daily practice queue based on what is due, unsolved, or needs more work.

Production: https://abhiy.xyz/leetdrill

## Development

Prereqs: Go 1.22+, Docker, and [Task](https://taskfile.dev).

```sh
cp .env.example .env
# fill LEETDRILL_COOKIE_KEY: openssl rand -base64 32

task install:tools
task db:up
task migrate:up
task test
task dev
```

Open http://localhost:8080.

## Deploy

`scripts/deploy_server.sh` bootstraps or updates an Ubuntu-style VPS. It syncs
source, preserves the remote `.env` by default, runs tests, builds binaries,
runs migrations, installs the systemd service, checks nginx, verifies HTTPS, and
publishes extension downloads.

```sh
task deploy:server -- abhiy.xyz
```

First-run example:

```sh
SETUP_POSTGRES=true \
DB_NAME=leetdrill \
DB_USER=leetdrill \
DB_PASSWORD='change-me' \
LETSENCRYPT_EMAIL=you@example.com \
task deploy:server -- new-host.example.com
```

Use `UPLOAD_ENV=true` only when you intentionally want to replace the remote
`.env`.

## Extension Build

Build Chrome and Firefox packages:

```sh
task extension:package
```

Build and publish extension downloads to the VPS:

```sh
task extension:deploy
```
