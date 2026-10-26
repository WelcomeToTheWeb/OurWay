#!/usr/bin/env bash
# Local e2e stack: PostgreSQL database + Go server + the production bundle on
# `vite preview`. The Playwright suite runs against these, through the product's
# own APIs — no mocks. See CONTRIBUTING.md for the run book.
#
#   scripts/e2e-stack.sh restart    # fresh DB, server, preview
#   scripts/e2e-stack.sh stop
set -euo pipefail

API_PORT="${API_PORT:-9099}"
WEB_PORT="${WEB_PORT:-3100}"
DB="${DB:-ourway_e2e}"
RUN_ID="${OURWAY_E2E_RUN_ID:-}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

stop() {
  pkill -f "vite preview --port ${WEB_PORT}" 2>/dev/null || true
  pkill -f "ourway-e2e[-]server" 2>/dev/null || true
  pkill -x ourway 2>/dev/null || true
}

case "${1:-restart}" in
stop)
  stop
  echo "stack stopped"
  exit 0
  ;;
esac

stop
sleep 1

docker exec ourway-pg psql -U ourway -d postgres -qc \
  "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${DB}';" >/dev/null 2>&1 || true
docker exec ourway-pg psql -U ourway -d postgres -qc "DROP DATABASE IF EXISTS ${DB};" >/dev/null
docker exec ourway-pg psql -U ourway -d postgres -qc "CREATE DATABASE ${DB} OWNER ourway;" >/dev/null
echo "database ${DB} recreated"

# No Go toolchain on this container, so the server is built in a Go container
# and run on the host (the same build CI runs, one step earlier).
GO_IMAGE="${GO_IMAGE:-golang:1.26}"
mkdir -p "$ROOT/.tmp"
if ! docker run --rm -v "$ROOT":/work -v go-mod:/go/pkg/mod -w /work/server \
  -e GOFLAGS=-buildvcs=false -e HOME=/tmp \
  "$GO_IMAGE" go build -o /work/.tmp/ourway-e2e-server . >/tmp/ourway-e2e-server.log 2>&1; then
  echo "the server did not build; see /tmp/ourway-e2e-server.log" >&2
  exit 1
fi
(setsid nohup env \
  DATABASE_URL="postgresql://ourway:ourway@localhost:5432/${DB}?sslmode=disable" \
  SERVER_PORT=":${API_PORT}" \
  WEB_URL="http://localhost:${WEB_PORT}" \
  WS_ORIGINS="localhost:${WEB_PORT}" \
  REDIS_ENABLED=false \
  JWT_SECRET=devlocalsecret \
  ENROLLMENT_SECRET="${ENROLLMENT_SECRET-dev-enrollment-secret}" \
  RATE_LIMIT_PER_MINUTE=600 \
  RATE_LIMIT_BURST=60 \
  "$ROOT/.tmp/ourway-e2e-server" >/tmp/ourway-e2e-server.log 2>&1 &)

cd "$ROOT/web"
(setsid nohup env OURWAY_PROXY_TARGET="http://localhost:${API_PORT}" \
  npx vite preview --port "${WEB_PORT}" >/tmp/ourway-e2e-preview.log 2>&1 &)

for i in $(seq 1 60); do
  if curl -sf -m2 "http://localhost:${API_PORT}/health" >/dev/null && curl -sf -m2 "http://localhost:${WEB_PORT}/" >/dev/null; then
    echo "stack up: api http://localhost:${API_PORT} web http://localhost:${WEB_PORT}"
    exit 0
  fi
  sleep 2
done

echo "stack did not come up; see /tmp/ourway-e2e-server.log and /tmp/ourway-e2e-preview.log" >&2
exit 1
