#!/usr/bin/env bash
set -euo pipefail

HOST="${1:-${HOST:-abhiyadav.in}}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

DOMAIN="${DOMAIN:-$HOST}"
DOMAIN_ALIASES="${DOMAIN_ALIASES:-www.$DOMAIN}"
BASE_PATH="${BASE_PATH:-/leetdrill}"
APP_ADDR="${APP_ADDR:-127.0.0.1:8082}"
ENV_FILE="${ENV_FILE:-$ROOT/.env}"
UPLOAD_ENV="${UPLOAD_ENV:-missing}"
SETUP_SYSTEM="${SETUP_SYSTEM:-true}"
SETUP_POSTGRES="${SETUP_POSTGRES:-false}"
SETUP_NGINX="${SETUP_NGINX:-true}"
ENABLE_TLS="${ENABLE_TLS:-auto}"
RUN_MIGRATIONS="${RUN_MIGRATIONS:-true}"
DEPLOY_EXTENSION="${DEPLOY_EXTENSION:-true}"
REMOTE_TEST_CMD="${REMOTE_TEST_CMD:-go test ./...}"
GOOSE_VERSION="${GOOSE_VERSION:-v3.23.0}"
SHARED_DIR="${SHARED_DIR:-/var/www/html/shared/leetdrill-extension}"

require_local() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing local command: $1" >&2
    exit 1
  fi
}

quote() {
  printf "%q" "$1"
}

bool() {
  case "$1" in
    true|false) printf "%s" "$1" ;;
    *) echo "expected true/false, got: $1" >&2; exit 1 ;;
  esac
}

require_local ssh
require_local rsync
if [ "$DEPLOY_EXTENSION" = true ]; then
  require_local python3
fi

SETUP_SYSTEM="$(bool "$SETUP_SYSTEM")"
SETUP_POSTGRES="$(bool "$SETUP_POSTGRES")"
SETUP_NGINX="$(bool "$SETUP_NGINX")"
RUN_MIGRATIONS="$(bool "$RUN_MIGRATIONS")"
DEPLOY_EXTENSION="$(bool "$DEPLOY_EXTENSION")"

REMOTE_USER="$(ssh "$HOST" 'id -un')"
REMOTE_GROUP="$(ssh "$HOST" 'id -gn')"
APP_DIR="${APP_DIR:-/home/$REMOTE_USER/leetdrill}"

echo "deploying leetdrill to $HOST:$APP_DIR"

ssh "$HOST" \
  "APP_DIR=$(quote "$APP_DIR") SETUP_SYSTEM=$SETUP_SYSTEM SETUP_POSTGRES=$SETUP_POSTGRES REMOTE_USER=$(quote "$REMOTE_USER") REMOTE_GROUP=$(quote "$REMOTE_GROUP") bash -s" <<'REMOTE_BOOTSTRAP'
set -euo pipefail

if [ "$SETUP_SYSTEM" = true ]; then
  if ! sudo -n true >/dev/null 2>&1; then
    echo "passwordless sudo is required for first-run setup" >&2
    exit 1
  fi

  if command -v apt-get >/dev/null 2>&1; then
    packages=(ca-certificates curl rsync unzip nginx certbot python3-certbot-nginx postgresql-client)
    if ! command -v go >/dev/null 2>&1; then
      packages+=(golang-go)
    fi
    if [ "$SETUP_POSTGRES" = true ]; then
      packages+=(postgresql)
    fi

    missing=()
    for package in "${packages[@]}"; do
      if ! dpkg -s "$package" >/dev/null 2>&1; then
        missing+=("$package")
      fi
    done
    if [ "${#missing[@]}" -gt 0 ]; then
      sudo apt-get update
      sudo DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}"
    fi
  fi
fi

if ! command -v go >/dev/null 2>&1; then
  echo "go is not installed on remote host" >&2
  exit 1
fi

mkdir -p "$APP_DIR/bin"
sudo mkdir -p /var/www/html/shared /var/www/letsencrypt
sudo chown -R "$REMOTE_USER:$REMOTE_GROUP" /var/www/html/shared

if [ "$SETUP_POSTGRES" = true ]; then
  sudo systemctl enable --now postgresql
fi
REMOTE_BOOTSTRAP

rsync -az --delete \
  --exclude='.git/' \
  --exclude='.env' \
  --exclude='bin/' \
  --exclude='dist/' \
  "$ROOT"/ "$HOST:$APP_DIR"/

case "$UPLOAD_ENV" in
  true|false|missing) ;;
  *) echo "UPLOAD_ENV must be true, false, or missing" >&2; exit 1 ;;
esac

remote_env_exists="$(ssh "$HOST" "test -f $(quote "$APP_DIR")/.env && echo true || echo false")"
if [ -f "$ENV_FILE" ] && { [ "$UPLOAD_ENV" = true ] || { [ "$UPLOAD_ENV" = missing ] && [ "$remote_env_exists" = false ]; }; }; then
  rsync -az "$ENV_FILE" "$HOST:$APP_DIR/.env"
  ssh "$HOST" "chmod 600 $(quote "$APP_DIR")/.env"
fi

ssh "$HOST" \
  "APP_DIR=$(quote "$APP_DIR") APP_ADDR=$(quote "$APP_ADDR") BASE_PATH=$(quote "$BASE_PATH") DOMAIN=$(quote "$DOMAIN") DOMAIN_ALIASES=$(quote "$DOMAIN_ALIASES") SETUP_POSTGRES=$SETUP_POSTGRES SETUP_NGINX=$SETUP_NGINX ENABLE_TLS=$(quote "$ENABLE_TLS") RUN_MIGRATIONS=$RUN_MIGRATIONS REMOTE_TEST_CMD=$(quote "$REMOTE_TEST_CMD") GOOSE_VERSION=$(quote "$GOOSE_VERSION") REMOTE_USER=$(quote "$REMOTE_USER") REMOTE_GROUP=$(quote "$REMOTE_GROUP") DATABASE_URL=$(quote "${DATABASE_URL:-}") LEETDRILL_COOKIE_KEY=$(quote "${LEETDRILL_COOKIE_KEY:-}") DB_NAME=$(quote "${DB_NAME:-}") DB_USER=$(quote "${DB_USER:-}") DB_PASSWORD=$(quote "${DB_PASSWORD:-}") LETSENCRYPT_EMAIL=$(quote "${LETSENCRYPT_EMAIL:-}") FORCE_NGINX_SITE=$(quote "${FORCE_NGINX_SITE:-false}") bash -s" <<'REMOTE_DEPLOY'
set -euo pipefail

sql_escape() {
  printf "%s" "$1" | sed "s/'/''/g"
}

if [ "$SETUP_POSTGRES" = true ] && [ -n "${DB_NAME:-}" ] && [ -n "${DB_USER:-}" ] && [ -n "${DB_PASSWORD:-}" ]; then
  case "$DB_NAME$DB_USER" in
    *[!a-zA-Z0-9_]*)
      echo "DB_NAME and DB_USER may only contain letters, digits, and underscore" >&2
      exit 1
      ;;
  esac
  escaped_password="$(sql_escape "$DB_PASSWORD")"
  sudo -u postgres psql -v ON_ERROR_STOP=1 -qAt <<SQL
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '$DB_USER') THEN
    CREATE ROLE "$DB_USER" LOGIN PASSWORD '$escaped_password';
  END IF;
END
\$\$;
SELECT 'CREATE DATABASE "$DB_NAME" OWNER "$DB_USER"'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$DB_NAME')\gexec
SQL
fi

if [ ! -f "$APP_DIR/.env" ]; then
  if [ -z "${DATABASE_URL:-}" ]; then
    if [ "$SETUP_POSTGRES" = true ] && [ -n "${DB_NAME:-}" ] && [ -n "${DB_USER:-}" ] && [ -n "${DB_PASSWORD:-}" ]; then
      DATABASE_URL="postgres://$DB_USER:$DB_PASSWORD@localhost:5432/$DB_NAME?sslmode=disable"
    else
      echo "remote .env missing; provide ENV_FILE, DATABASE_URL, or SETUP_POSTGRES=true DB_NAME/DB_USER/DB_PASSWORD" >&2
      exit 1
    fi
  fi
  cookie_key="${LEETDRILL_COOKIE_KEY:-$(openssl rand -base64 32)}"
  umask 077
  cat > "$APP_DIR/.env" <<ENV
LEETDRILL_ADDR=$APP_ADDR
DATABASE_URL=$DATABASE_URL
LEETDRILL_COOKIE_KEY=$cookie_key
SINGLE_USER=false
LEETDRILL_BASE_PATH=$BASE_PATH
LEETDRILL_SECURE_COOKIES=true
LEETDRILL_SYNC_WORKER=true
ENV
fi
chmod 600 "$APP_DIR/.env"

set -a
. "$APP_DIR/.env"
set +a
effective_addr="${LEETDRILL_ADDR:-$APP_ADDR}"

cd "$APP_DIR"
$REMOTE_TEST_CMD
go build -o bin/server ./cmd/server
go build -o bin/ingest ./cmd/ingest

if [ "$RUN_MIGRATIONS" = true ]; then
  go run "github.com/pressly/goose/v3/cmd/goose@$GOOSE_VERSION" -dir migrations postgres "$DATABASE_URL" up
fi

sudo tee /etc/systemd/system/leetdrill.service >/dev/null <<SERVICE
[Unit]
Description=LeetDrill web server
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=$REMOTE_USER
Group=$REMOTE_GROUP
WorkingDirectory=$APP_DIR
EnvironmentFile=$APP_DIR/.env
ExecStart=$APP_DIR/bin/server
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
SERVICE

sudo systemctl daemon-reload
sudo systemctl enable leetdrill.service >/dev/null
sudo systemctl restart leetdrill.service

if [ "$SETUP_NGINX" = true ]; then
  if ! command -v nginx >/dev/null 2>&1; then
    echo "nginx is not installed on remote host" >&2
    exit 1
  fi

  if sudo grep -Rqs "location $BASE_PATH/" /etc/nginx/sites-available /etc/nginx/sites-enabled; then
    echo "nginx already has $BASE_PATH location; leaving existing site config in place"
  else
    site="/etc/nginx/sites-available/$DOMAIN"
    if [ -e "$site" ] && [ "${FORCE_NGINX_SITE:-false}" != true ]; then
      echo "$site exists but does not contain $BASE_PATH; set FORCE_NGINX_SITE=true to overwrite it" >&2
      exit 1
    fi

    server_names="$DOMAIN $DOMAIN_ALIASES"
    sudo tee "$site" >/dev/null <<NGINX
server {
    listen 80;
    listen [::]:80;
    server_name $server_names;
    root /var/www/html;

    location /.well-known/acme-challenge/ {
        root /var/www/letsencrypt;
    }

    location = $BASE_PATH {
        return 301 $BASE_PATH/;
    }

    location $BASE_PATH/ {
        proxy_pass http://$effective_addr/;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header X-Forwarded-Prefix $BASE_PATH;
    }

    location / {
        try_files \$uri \$uri/ =404;
    }
}
NGINX
    sudo ln -sfn "$site" "/etc/nginx/sites-enabled/$DOMAIN"
  fi

  sudo nginx -t
  sudo systemctl reload nginx

  cert_path="/etc/letsencrypt/live/$DOMAIN/fullchain.pem"
  cert_exists=false
  if sudo test -f "$cert_path"; then
    cert_exists=true
  fi
  if { [ "$ENABLE_TLS" = true ] || { [ "$ENABLE_TLS" = auto ] && [ "$cert_exists" = false ]; }; } && [ "$cert_exists" = false ]; then
    if [ -z "${LETSENCRYPT_EMAIL:-}" ]; then
      echo "TLS certificate missing; set LETSENCRYPT_EMAIL to let deploy run certbot" >&2
      exit 1
    fi
    certbot_domains=(-d "$DOMAIN")
    for alias in $DOMAIN_ALIASES; do
      certbot_domains+=(-d "$alias")
    done
    sudo certbot --nginx --non-interactive --agree-tos --redirect -m "$LETSENCRYPT_EMAIL" "${certbot_domains[@]}"
  fi
fi

systemctl is-active leetdrill.service
curl -fsS "http://$effective_addr/healthz" >/dev/null
REMOTE_DEPLOY

if [ "$DEPLOY_EXTENSION" = true ]; then
  python3 "$ROOT/scripts/check_extensions.py"
  python3 "$ROOT/scripts/package_extension.py" --out "$ROOT/dist/extension-share"
  ssh "$HOST" "mkdir -p $(quote "$SHARED_DIR")"
  rsync -az --delete "$ROOT/dist/extension-share"/ "$HOST:$SHARED_DIR"/
fi

if [ -d "$ROOT/web/homepage" ]; then
  python3 "$ROOT/scripts/check_homepage.py"
  tmp_home="/tmp/leetdrill-homepage"
  ssh "$HOST" "rm -rf $(quote "$tmp_home") && mkdir -p $(quote "$tmp_home")"
  rsync -az --delete "$ROOT/web/homepage"/ "$HOST:$tmp_home"/
  ssh "$HOST" "sudo rsync -a --delete --exclude=shared/ $(quote "$tmp_home")/ /var/www/html/ && rm -rf $(quote "$tmp_home")"
fi

scheme="https"
if [ "$ENABLE_TLS" = false ]; then
  scheme="http"
fi

curl -fsS "$scheme://$DOMAIN$BASE_PATH/healthz" >/dev/null
if [ -d "$ROOT/web/homepage" ]; then
  curl -fsS "$scheme://$DOMAIN/" >/dev/null
fi
echo "deployed: $scheme://$DOMAIN$BASE_PATH/"
if [ -d "$ROOT/web/homepage" ]; then
  echo "homepage: $scheme://$DOMAIN/"
fi
if [ "$DEPLOY_EXTENSION" = true ]; then
  echo "extensions: $scheme://$DOMAIN/shared/leetdrill-extension/"
fi
