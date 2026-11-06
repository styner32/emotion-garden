#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

./scripts/dev-db.sh start

if [ ! -x ./bin/migrate ] || [ ! -x ./bin/server ]; then
  go build -o ./bin/migrate ./cmd/migrate
  go build -o ./bin/server ./cmd/server
fi

./bin/migrate up

export HTTP_ADDR="${HTTP_ADDR:-:8080}"
export PUBLIC_ORIGIN="${PUBLIC_ORIGIN:-http://localhost:8080}"
export EXTRA_ALLOWED_ORIGINS="${EXTRA_ALLOWED_ORIGINS:-http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173}"
export ALLOW_LOOPBACK_PREVIEW_TUNNEL="true"
export ALLOW_MOCK_AI="${ALLOW_MOCK_AI:-true}"

exec ./bin/server
