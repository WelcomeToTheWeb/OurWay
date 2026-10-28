#!/usr/bin/env bash
# 2.3-8 — the tenancy scale run.
#
# What this proves, in the plan's words: 10,000 devices, a technician scoped to
# one tenant (2,000 units), console reads sustained at ~100 req/s, zero
# failures, and the scoped list's p95 within 10% of the unscoped baseline.
#
# What it does not prove: the console's per-user request budget. The server
# answers 100 requests per minute per user (2.2-3), and a 100 req/s campaign is
# above that by design, so the run raises RATE_LIMIT_PER_MINUTE. It is measuring
# the cost of the scoped read, not the limiter's refusal — the budget itself is
# asserted in the api suite, where it belongs.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH=/usr/local/go/bin:$PATH

DEVICES="${SCALE_DEVICES:-10000}"
TENANTS="${SCALE_TENANTS:-5}"
RPS="${SCALE_RPS:-100}"
SECONDS_PER_ACTOR="${SCALE_CONSOLE_SECONDS:-30}"
API_PORT="${SCALE_API_PORT:-9098}"
DB="${SCALE_DB:-ourway_scale}"
PG="${SCALE_PG_CONTAINER:-ourway-pg}"
SERVER="$ROOT/.tmp/ourway-scale-server"
LOG=/tmp/ourway-scale-server.log

say() { printf 'scale: %s\n' "$*"; }

if ! docker inspect "$PG" >/dev/null 2>&1; then
  say "the Postgres container '$PG' is not running; scripts/e2e-stack.sh starts one"
  exit 1
fi

say "recreating database $DB"
docker exec "$PG" psql -U ourway -d postgres -qc "DROP DATABASE IF EXISTS $DB;" >/dev/null
docker exec "$PG" psql -U ourway -d postgres -qc "CREATE DATABASE $DB OWNER ourway;" >/dev/null

say "building the server and the scale tool"
mkdir -p "$ROOT/.tmp"
( cd "$ROOT/server" && go build -o "$SERVER" . )
( cd "$ROOT/server" && go build -tags loadtest -o "$ROOT/.tmp/ourway-scale-tool" ./loadtest/scale )

say "starting the server on :$API_PORT (rate limit raised for the measurement)"
(setsid nohup env \
  DATABASE_URL="postgresql://ourway:ourway@localhost:5432/${DB}?sslmode=disable" \
  SERVER_PORT=":${API_PORT}" \
  WEB_URL="http://localhost:3100" \
  WS_ORIGINS="localhost:3100" \
  REDIS_ENABLED=false \
  JWT_SECRET=scalesecret \
  ENROLLMENT_SECRET=dev-enrollment-secret \
  RATE_LIMIT_PER_MINUTE="${SCALE_RATE_LIMIT:-1000000}" \
  "$SERVER" >"$LOG" 2>&1 &)

cleanup() {
  pkill -f "$SERVER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for i in $(seq 1 60); do
  if curl -sf -m2 "http://localhost:${API_PORT}/health" >/dev/null; then break; fi
  sleep 1
done
curl -sf -m2 "http://localhost:${API_PORT}/health" >/dev/null || { say "server did not come up; see $LOG"; exit 1; }

say "running the scale tool: $DEVICES devices, $TENANTS tenants, ${RPS} req/s for ${SECONDS_PER_ACTOR}s per actor"
"$ROOT/.tmp/ourway-scale-tool" \
  -url "http://localhost:${API_PORT}" \
  -devices "$DEVICES" \
  -tenants "$TENANTS" \
  -console-rps "$RPS" \
  -console-seconds "$SECONDS_PER_ACTOR" \
  -console-workers 20 \
  -skip-simulation \
  -duration 1s
status=$?

say "server log tail"
tail -5 "$LOG" || true
exit $status
