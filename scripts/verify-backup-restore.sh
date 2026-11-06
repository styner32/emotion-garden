#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PG_BIN="/usr/lib/postgresql/15/bin"
if [ ! -d "$PG_BIN" ]; then
  PG_BIN="$(dirname "$(command -v pg_dump)")"
fi

PGHOST="${PGHOST:-127.0.0.1}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-app}"
PGPASSWORD="${PGPASSWORD:-change_me}"
export PGPASSWORD

SRC_DB="${PGDATABASE:-emotion_garden}"
RESTORE_DB="emotion_garden_restore_verify_$$"
DUMP_FILE="$ROOT_DIR/.local/backup_verify_$$.dump"

cleanup() {
  rm -f "$DUMP_FILE"
  psql -h "$PGHOST" -p "$PGPORT" -U postgres -d postgres -c "DROP DATABASE IF EXISTS $RESTORE_DB;" >/dev/null 2>&1 || true
}
trap cleanup EXIT

"$ROOT_DIR/scripts/dev-db.sh" start >/dev/null
(cd "$ROOT_DIR" && ./bin/migrate up >/dev/null)

echo "[backup-verify] Dumping $SRC_DB to custom archive..."
"$PG_BIN/pg_dump" -Fc -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$SRC_DB" -f "$DUMP_FILE"

echo "[backup-verify] Creating isolated restore database $RESTORE_DB..."
psql -h "$PGHOST" -p "$PGPORT" -U postgres -d postgres -c "CREATE DATABASE $RESTORE_DB OWNER $PGUSER;" >/dev/null

echo "[backup-verify] Restoring archive into $RESTORE_DB..."
"$PG_BIN/pg_restore" -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$RESTORE_DB" --no-owner --role="$PGUSER" "$DUMP_FILE"

echo "[backup-verify] Running ./bin/migrate verify against restored database..."
(
  cd "$ROOT_DIR"
  DATABASE_URL="postgres://$PGUSER:$PGPASSWORD@$PGHOST:$PGPORT/$RESTORE_DB?sslmode=disable" ./bin/migrate verify
)

echo "[backup-verify] Backup & restore rehearsal PASSED."
