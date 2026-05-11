#!/usr/bin/env bash
set -euo pipefail

HOST="${1:-abhiy.xyz}"
REMOTE_DIR="${2:-/var/www/html/shared/leetdrill-extension}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist/extension-share"

python3 "$ROOT/scripts/check_extensions.py"
python3 "$ROOT/scripts/package_extension.py" --out "$DIST"

ssh "$HOST" "mkdir -p '$REMOTE_DIR'"
rsync -az --delete "$DIST"/ "$HOST:$REMOTE_DIR"/

echo "https://$HOST/shared/leetdrill-extension/"
