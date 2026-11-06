#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PG_BIN="/usr/lib/postgresql/15/bin"
if [ ! -d "$PG_BIN" ]; then
  PG_BIN="$(dirname "$(command -v pg_ctl)")"
fi

PGDATA="${PGDATA:-$ROOT_DIR/.local/pgdata}"
PGPORT="${PGPORT:-5432}"
PGHOST="${PGHOST:-127.0.0.1}"
PGUSER="${PGUSER:-app}"
PGDATABASE="${PGDATABASE:-emotion_garden}"
PGLOG="$ROOT_DIR/.local/postgres.log"
PGSOCK="$ROOT_DIR/.local/pgsock"

mkdir -p "$ROOT_DIR/.local" "$PGSOCK"

cmd="${1:-status}"

init_cluster() {
  if [ ! -f "$PGDATA/PG_VERSION" ]; then
    echo "[dev-db] Initializing PostgreSQL cluster at $PGDATA..."
    "$PG_BIN/initdb" -D "$PGDATA" -U postgres --auth=trust --encoding=UTF8 --locale=C >/dev/null
    cat >> "$PGDATA/postgresql.conf" <<EOF
listen_addresses = '127.0.0.1'
port = $PGPORT
unix_socket_directories = '$PGSOCK'
max_connections = 100
shared_buffers = 128MB
timezone = 'UTC'
log_timezone = 'UTC'
EOF
  fi
}

start_cluster() {
  init_cluster
  if "$PG_BIN/pg_isready" -h "$PGHOST" -p "$PGPORT" >/dev/null 2>&1; then
    echo "[dev-db] PostgreSQL already running on $PGHOST:$PGPORT"
  else
    echo "[dev-db] Starting PostgreSQL on $PGHOST:$PGPORT..."
    "$PG_BIN/pg_ctl" -D "$PGDATA" -l "$PGLOG" -w start >/dev/null
  fi

  psql -h "$PGHOST" -p "$PGPORT" -U postgres -d postgres -tc "SELECT 1 FROM pg_roles WHERE rolname = '$PGUSER'" | grep -q 1 || \
    psql -h "$PGHOST" -p "$PGPORT" -U postgres -d postgres -c "CREATE ROLE $PGUSER WITH LOGIN PASSWORD 'change_me' CREATEDB;" >/dev/null

  psql -h "$PGHOST" -p "$PGPORT" -U postgres -d postgres -tc "SELECT 1 FROM pg_database WHERE datname = '$PGDATABASE'" | grep -q 1 || \
    psql -h "$PGHOST" -p "$PGPORT" -U postgres -d postgres -c "CREATE DATABASE $PGDATABASE OWNER $PGUSER;" >/dev/null

  echo "[dev-db] Ready: postgres://$PGUSER:change_me@$PGHOST:$PGPORT/$PGDATABASE?sslmode=disable"
}

stop_cluster() {
  if [ -f "$PGDATA/PG_VERSION" ] && "$PG_BIN/pg_isready" -h "$PGHOST" -p "$PGPORT" >/dev/null 2>&1; then
    "$PG_BIN/pg_ctl" -D "$PGDATA" -m fast -w stop >/dev/null
    echo "[dev-db] PostgreSQL stopped."
  else
    echo "[dev-db] PostgreSQL is not running."
  fi
}

case "$cmd" in
  start)
    start_cluster
    ;;
  stop)
    stop_cluster
    ;;
  restart)
    stop_cluster
    start_cluster
    ;;
  status)
    "$PG_BIN/pg_isready" -h "$PGHOST" -p "$PGPORT"
    ;;
  reset)
    stop_cluster
    rm -rf "$PGDATA"
    start_cluster
    ;;
  *)
    echo "Usage: $0 {start|stop|restart|status|reset}"
    exit 1
    ;;
esac
