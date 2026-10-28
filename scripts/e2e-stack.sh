#!/usr/bin/env bash
# Local e2e stack: PostgreSQL database + Go server + the production bundle on
# `vite preview`. The Playwright suite runs against these, through the product's
# own APIs — no mocks. See CONTRIBUTING.md for the run book.
#
#   scripts/e2e-stack.sh restart    # fresh DB, server, preview
#   scripts/e2e-stack.sh test       # restart, then run the Playwright suite
#   scripts/e2e-stack.sh stop
#
# Two shapes, one script. `REDIS_ENABLED=true REDIS_URL=… scripts/e2e-stack.sh
# test` is the shape CI runs (the fleet cache, the rate limiter and the WS
# fan-out live in Redis); the default is Redis off, which is the single-box
# install. Run the Redis shape before saying a change is green.
#
# The suite's seed is create-if-missing, so a run inherits whatever the previous
# run left behind on that database — acknowledged alerts, approved updates,
# enrolled units under the C4 re-registration guard. That is why `test` recreates
# the database *and* passes a run id: with `OURWAY_E2E_RUN_ID` the seed is
# run-scoped (unit names, hostnames, IPs and update titles carry the id), so two
# runs against one database seed different machines and assert different rows.
# Running the suite by hand against a stack you have already tested against is
# the one way to make it fail for reasons that are not in the diff.
set -euo pipefail

API_PORT="${API_PORT:-9099}"
WEB_PORT="${WEB_PORT:-3100}"
DB="${DB:-ourway_e2e}"
REDIS_ENABLED="${REDIS_ENABLED:-false}"
REDIS_URL="${REDIS_URL:-redis://localhost:6379/0}"
# A run id that is name-safe and unique per invocation; `date` to the second is
# enough because the suite takes about a minute.
RUN_ID="${OURWAY_E2E_RUN_ID:-$(date -u +%Y%m%d%H%M%S)}"
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
test)
  "$0" restart
  cd "$ROOT/web"
  echo "suite run id: ${RUN_ID}"
  OURWAY_E2E_RUN_ID="${RUN_ID}" \
    OURWAY_E2E_BASE_URL="http://localhost:${WEB_PORT}" \
    OURWAY_E2E_API_URL="http://localhost:${API_PORT}" \
    OURWAY_E2E_ENROLLMENT_SECRET="${ENROLLMENT_SECRET-dev-enrollment-secret}" \
    npm run test:e2e -- --reporter=list
  ;;
esac

stop
sleep 1

docker exec ourway-pg psql -U ourway -d postgres -qc \
  "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${DB}';" >/dev/null 2>&1 || true
docker exec ourway-pg psql -U ourway -d postgres -qc "DROP DATABASE IF EXISTS ${DB};" >/dev/null
docker exec ourway-pg psql -U ourway -d postgres -qc "CREATE DATABASE ${DB} OWNER ourway;" >/dev/null
if [ "${REDIS_ENABLED}" = "true" ]; then
  # The fleet cache and the rate limiter live in Redis: a database recreated
  # with a Redis that still holds the previous run's cache is half a reset.
  docker exec ourway-redis redis-cli flushall >/dev/null
  echo "database ${DB} recreated, redis flushed"
else
  echo "database ${DB} recreated"
fi

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
  REDIS_ENABLED="${REDIS_ENABLED}" \
  REDIS_URL="${REDIS_URL}" \
  JWT_SECRET=devlocalsecret \
  ENROLLMENT_SECRET="${ENROLLMENT_SECRET-dev-enrollment-secret}" \
  RATE_LIMIT_PER_MINUTE=600 \
  RATE_LIMIT_BURST=60 \
  "$ROOT/.tmp/ourway-e2e-server" >/tmp/ourway-e2e-server.log 2>&1 &)

cd "$ROOT/web"
# Build, then serve `dist`. `vite preview` serves whatever bundle is on disk and
# builds nothing, so a console change is invisible to the suite until this runs —
# and the failure reads as "the console has no such control", not "the bundle is
# stale". Recorded in docs/phase2.3-tasks.md as a harness fact; the loop enforces
# it.
if ! npm run build >/tmp/ourway-e2e-build.log 2>&1; then
  echo "the console bundle did not build; see /tmp/ourway-e2e-build.log" >&2
  exit 1
fi
(setsid nohup env OURWAY_PROXY_TARGET="http://localhost:${API_PORT}" \
  npx vite preview --port "${WEB_PORT}" >/tmp/ourway-e2e-preview.log 2>&1 &)

for i in $(seq 1 60); do
  if curl -sf -m2 "http://localhost:${API_PORT}/health" >/dev/null && curl -sf -m2 "http://localhost:${WEB_PORT}/" >/dev/null; then
    echo "stack up: api http://localhost:${API_PORT} web http://localhost:${WEB_PORT} (redis ${REDIS_ENABLED}, run id ${RUN_ID})"
    exit 0
  fi
  sleep 2
done

echo "stack did not come up; see /tmp/ourway-e2e-server.log and /tmp/ourway-e2e-preview.log" >&2
exit 1
