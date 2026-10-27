#!/usr/bin/env bash
#
# 2.2-0 — agent smoke: run the *real agent binary* against a real server and
# prove the product's own path works on the platform the job runs on.
#
#   scripts/agent-smoke.sh --server http://localhost:9090 [options]
#
#   --server URL     server REST origin (required)
#   --ws URL         agent WebSocket origin (default: the --server origin with ws://)
#   --agent PATH     agent binary (default: ./ourway-agent)
#   --name NAME      unit name to enroll (default: smoke-<os>)
#   --secret SECRET  enrollment secret, sent as X-Enrollment-Secret
#   --expect-frames  require a remote-session frame (a session reaching "active")
#   --expect-service require the installed service to report a heartbeat
#                    (systemd / launchd / Windows service; run as root/admin)
#   --expect-capability=VALUE  require the unit to report this input capability
#                    (granted/denied/unavailable/unknown), or `reported` for
#                    "the agent stated a decision" (granted *or* denied) —
#                    which is what a cgo darwin build must do in CI
#   --expect-latency-ms=MS  fail if the measured server→agent control round
#                    trip (one key event) exceeds MS milliseconds
#
# What it asserts, in the order a real install experiences it:
#   1. the unit enrolls          (POST /api/agent/register)
#   2. the agent connects        (device status becomes "online" — hub registration)
#   3. heartbeats land           (last_seen advances past enrollment)
#   4. metrics land              (the metric history for the unit is non-empty)
#   5. remote control works      (--expect-frames: a session reaches "active",
#                                 which only happens when the agent ships a frame)
#   6. the service runs          (--expect-service: after --install, the installed
#                                 service reports a heartbeat of its own)
#   7. control latency is measured   (POST a key event → the agent acts on it,
#      stamped by the wall clock: the number the acceptance criteria asks for)
#   8. the unit states what remote control can do (--expect-capability): the
#      agent reports its own input capability, so the console never has to
#      guess. On the macOS runner the assertion is `reported`, not `denied`:
#      GitHub's macOS images ship with Accessibility already granted to the
#      runner user, so AXIsProcessTrusted() answers true there and a build that
#      says so is behaving correctly. The point of the assertion is that a cgo
#      darwin build *states a decision* (granted/denied) where a CGO_ENABLED=0
#      build can only say `unavailable` — not that the runner happens to be
#      ungranted.
#
# Screen capture needs a display: pass --expect-frames only where one exists
# (Linux with Xvfb + scrot, Windows with the runner desktop). On macOS a CI
# runner cannot grant Screen Recording (TCC) non-interactively, so the frame
# assertion is deliberately not made there — the macOS control path is the
# manual gate in docs/phase2.2-tasks.md (2.2-1), and this script says so out
# loud instead of pretending.
set -euo pipefail

SERVER="" WS="" AGENT="./ourway-agent" NAME="" SECRET="" EXPECT_FRAMES=0 EXPECT_SERVICE=0
EXPECT_CAPABILITY=""
EXPECT_LATENCY_MS=""
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
# Root privilege: macOS/Linux need sudo; the Windows runner is already
# administrator, and Git Bash has no sudo at all.
case "$OS" in
  mingw*|msys*|cygwin*) SUDO="" ;;
  *) SUDO="sudo" ;;
esac

while [ $# -gt 0 ]; do
  case "$1" in
    --server) SERVER="$2"; shift 2 ;;
    --ws) WS="$2"; shift 2 ;;
    --agent) AGENT="$2"; shift 2 ;;
    --name) NAME="$2"; shift 2 ;;
    --secret) SECRET="$2"; shift 2 ;;
    --expect-frames) EXPECT_FRAMES=1; shift ;;
    --expect-service) EXPECT_SERVICE=1; shift ;;
    --expect-capability=*) EXPECT_CAPABILITY="${1#--expect-capability=}"; shift ;;
    --expect-latency-ms=*) EXPECT_LATENCY_MS="${1#--expect-latency-ms=}"; shift ;;
    *) echo "agent-smoke: unknown argument: $1" >&2; exit 2 ;;
  esac
done

[ -n "$SERVER" ] || { echo "agent-smoke: --server is required" >&2; exit 2; }
NAME=${NAME:-"smoke-$OS"}
WS=${WS:-${SERVER/http:\/\//ws://}}
[ -x "$AGENT" ] || { echo "agent-smoke: agent binary not found/executable: $AGENT" >&2; exit 2; }

PASS=0
FAIL=0
LOG=$(mktemp); AGENT_LOG=$(mktemp)
AGENT_PID=""
cleanup() {
  [ -n "$AGENT_PID" ] && kill "$AGENT_PID" 2>/dev/null || true
  rm -f "$LOG" "$AGENT_LOG"
}
trap cleanup EXIT INT TERM

ok() { echo "  ✓ $1"; PASS=$((PASS + 1)); }
no() { echo "  ✗ $1"; FAIL=$((FAIL + 1)); }
step() { echo "[smoke] $1"; }

json() { # $1 = a python expression over `d`, the parsed JSON body
  python3 -c "import sys, json
d = json.load(sys.stdin)
print(eval(sys.argv[1], {'d': d, 'len': len, 'str': str}))" "$1" 2>/dev/null || echo ""
}
ENROLL_ARGS=()
[ -n "$SECRET" ] && ENROLL_ARGS=(-H "X-Enrollment-Secret: $SECRET")

echo "========================================"
echo "  OurWay agent smoke ($OS)"
echo "  server: $SERVER   ws: $WS   agent: $AGENT"
echo "========================================"

# --- 1. an operator account exists (first registration is the product's bootstrap)
step "ensuring an operator account"
curl -fsS -o /dev/null "$SERVER/ready" || { echo "agent-smoke: server not ready at $SERVER" >&2; exit 1; }
curl -s -o "$LOG" -X POST "$SERVER/api/auth/register" -H 'content-type: application/json' \
  -d '{"username":"smoke-admin","email":"smoke-admin@example.invalid","password":"Smoke-Admin-Pass-2026!"}' >/dev/null || true
TOKEN=$(curl -s -X POST "$SERVER/api/auth/login" -H 'content-type: application/json' \
  -d '{"username":"smoke-admin","password":"Smoke-Admin-Pass-2026!"}' | json "d['access_token']")
[ -n "$TOKEN" ] && ok "operator account ready" || { no "could not log in as the smoke operator"; echo "aborting"; exit 1; }

AUTH=(-H "Authorization: Bearer $TOKEN")

# --- 2. the unit enrolls through the agent-facing API
step "enrolling unit $NAME"
HOST="smoke-$OS-$$"
REG=$(curl -s -X POST "$SERVER/api/agent/register" -H 'content-type: application/json' \
  ${ENROLL_ARGS[@]+"${ENROLL_ARGS[@]}"} \
  -d "{\"name\":\"$NAME\",\"hostname\":\"$HOST\",\"os\":\"$OS\",\"arch\":\"$(uname -m)\",\"agent_version\":\"smoke\"}")
DEVICE_ID=$(echo "$REG" | json "d['device']['id']")
DEVICE_KEY=$(echo "$REG" | json "d['device_key']")
if [ -n "$DEVICE_ID" ] && [ -n "$DEVICE_KEY" ]; then
  ok "unit enrolled ($DEVICE_ID)"
else
  no "unit enrollment failed: $REG"
  exit 1
fi

# --- 3. the agent runs and connects
step "starting the agent"
OURWAY_METRICS_INTERVAL=2s OURWAY_HEARTBEAT=3s \
  "$AGENT" -server "$WS" -key "$DEVICE_KEY" -device-id "$DEVICE_ID" -log-file "$AGENT_LOG" \
  >"$AGENT_LOG" 2>&1 &
AGENT_PID=$!

step "waiting for the unit to report online"
STATUS=""
for _ in $(seq 1 60); do
  STATUS=$(curl -s "${AUTH[@]}" "$SERVER/api/devices/$DEVICE_ID" | json "d['device']['status']")
  [ "$STATUS" = "online" ] && break
  if ! kill -0 "$AGENT_PID" 2>/dev/null; then break; fi
  sleep 1
done
if [ "$STATUS" = "online" ]; then ok "unit is online (device WebSocket registered)"; else
  no "unit never came online (status: ${STATUS:-none})"; echo "  agent log:"; tail -25 "$AGENT_LOG" | sed 's/^/    /'
fi

# --- 4. heartbeat and metrics land
step "waiting for heartbeat and metric samples"
LAST_SEEN=""; METRICS=0
for _ in $(seq 1 45); do
  LAST_SEEN=$(curl -s "${AUTH[@]}" "$SERVER/api/devices/$DEVICE_ID" | json "d['device']['last_seen']")
  METRICS=$(curl -s "${AUTH[@]}" "$SERVER/api/devices/$DEVICE_ID/metrics/history" | json "len(d['metrics'])" 2>/dev/null || echo 0)
  [ -n "$LAST_SEEN" ] && [ "${METRICS:-0}" -gt 0 ] && break
  sleep 1
done
[ -n "$LAST_SEEN" ] && ok "heartbeat recorded (last_seen $LAST_SEEN)" || no "heartbeat never recorded"
[ "${METRICS:-0}" -gt 0 ] && ok "metrics recorded ($METRICS samples)" || no "no metric samples arrived"

# --- 4b. the unit reports what remote control can do on it (2.2-1)
if [ -n "$EXPECT_CAPABILITY" ]; then
  step "waiting for the unit to report input_capability=$EXPECT_CAPABILITY"
  CAPABILITY=""
  for _ in $(seq 1 30); do
    CAPABILITY=$(curl -s "${AUTH[@]}" "$SERVER/api/devices/$DEVICE_ID" | json "d['device'].get('input_capability','')")
    if [ "$EXPECT_CAPABILITY" = "reported" ]; then
      case "$CAPABILITY" in granted|denied) break ;; esac
    elif [ "$CAPABILITY" = "$EXPECT_CAPABILITY" ]; then
      break
    fi
    sleep 1
  done
  if [ "$EXPECT_CAPABILITY" = "reported" ]; then
    case "$CAPABILITY" in
      granted|denied) ok "unit states its input capability ($CAPABILITY) — the console renders that word, never a guess" ;;
      *) no "unit reported input_capability='${CAPABILITY:-<empty>}' , expected a stated decision (granted/denied)" ;;
    esac
  else
    [ "$CAPABILITY" = "$EXPECT_CAPABILITY" ] \
      && ok "unit reports its input capability ($CAPABILITY) — the console states it" \
      || no "unit reported input_capability='${CAPABILITY:-<empty>}' , expected $EXPECT_CAPABILITY"
  fi
fi

# --- 5. remote control: a session reaches active, which needs a real frame
if [ "$EXPECT_FRAMES" = "1" ]; then
  step "starting a remote session"
  SESSION_ID=$(curl -s -X POST "$SERVER/api/devices/$DEVICE_ID/sessions" "${AUTH[@]}" | json "d['session']['id']")
  if [ -n "$SESSION_ID" ]; then
    SSTATE=""
    for _ in $(seq 1 60); do
      SSTATE=$(curl -s "${AUTH[@]}" "$SERVER/api/sessions" | python3 -c "
import sys, json
want = '$SESSION_ID'
for s in json.load(sys.stdin).get('sessions', []):
    if s['id'] == want:
        print(s['status']); break
" 2>/dev/null || echo "")
      [ "$SSTATE" = "active" ] && break
      sleep 1
    done
    if [ "$SSTATE" = "active" ]; then
      ok "remote session reached active — the agent shipped frames"
    else
      no "session never became active (status: ${SSTATE:-gone}) — capture produced no frames"
      echo "  agent log:"; tail -25 "$AGENT_LOG" | sed 's/^/    /'
    fi
    # Measured control latency: the time from POSTing a key event to the
    # server, to the agent acting on it (the session code stamps every key
    # it receives). This is the number 2.2-1's acceptance asks for, and the
    # one a technician feels as the pointer lagging behind the hand.
    step "measuring control round-trip"
    MARK="smoke-$RANDOM"
    SENT=$(python3 -c 'import time; print(time.time())')
    curl -s -o /dev/null -X POST "$SERVER/api/sessions/$SESSION_ID/input" "${AUTH[@]}" \
      -H 'content-type: application/json' \
      -d "{\"type\":\"key\",\"payload\":{\"event\":\"down\",\"key\":\"j\",\"code\":\"KeyJ\",\"note\":\"$MARK\"}}" || true
    LATENCY=""
    for _ in $(seq 1 200); do
      if grep -q "key event: down j" "$AGENT_LOG" 2>/dev/null; then
        LATENCY=$(python3 -c "import time; print(round(time.time() - $SENT, 3))")
        break
      fi
      sleep 0.05
    done
    if [ -n "$LATENCY" ]; then
      ok "key event reached the agent in ${LATENCY}s (server→agent, same host)"
      if [ -n "$EXPECT_LATENCY_MS" ] && python3 -c "import sys; sys.exit(0 if $LATENCY*1000 <= $EXPECT_LATENCY_MS else 1)"; then
        ok "control latency ${LATENCY}s within the ${EXPECT_LATENCY_MS} ms budget"
      elif [ -n "$EXPECT_LATENCY_MS" ]; then
        no "control latency ${LATENCY}s exceeds the ${EXPECT_LATENCY_MS} ms budget"
      fi
    else
      no "the agent never logged the key event (control path dead?)"
    fi

    curl -s -o /dev/null -X DELETE "$SERVER/api/sessions/$SESSION_ID" "${AUTH[@]}" || true
  else
    no "session start failed"
  fi
elif [ "$OS" = "darwin" ]; then
  echo "  – frame assertion skipped on macOS: Screen Recording (TCC) cannot be granted"
  echo "    non-interactively on a CI runner. The macOS control path is the manual gate"
  echo "    in docs/phase2.2-tasks.md 2.2-1; do not read this line as a pass."
fi

# --- 6. the installed service reports on its own
if [ "$EXPECT_SERVICE" = "1" ]; then
  step "installing and starting the service"
  kill "$AGENT_PID" 2>/dev/null || true; AGENT_PID=""
  sleep 2
  SMOKE_LOG=/tmp/ourway-smoke-agent.log
  if ! $SUDO "$AGENT" -server "$WS" -key "$DEVICE_KEY" -device-id "$DEVICE_ID" --install --yes >"$SMOKE_LOG" 2>&1; then
    no "service install failed"; tail -20 "$SMOKE_LOG" | sed 's/^/    /'
  else
  HEARTBEAT=""
  BEFORE=$(curl -s "${AUTH[@]}" "$SERVER/api/devices/$DEVICE_ID" | json "d['device']['last_seen']")
  for _ in $(seq 1 90); do
    NOW=$(curl -s "${AUTH[@]}" "$SERVER/api/devices/$DEVICE_ID" | json "d['device']['last_seen']")
    if [ -n "$NOW" ] && [ "$NOW" != "$BEFORE" ]; then HEARTBEAT="$NOW"; break; fi
    sleep 1
  done
  if [ -n "$HEARTBEAT" ]; then
    ok "installed service reports heartbeats (last_seen $HEARTBEAT)"
  else
    no "installed service produced no heartbeat"
    echo "  installer log:"; tail -20 "$SMOKE_LOG" | sed 's/^/    /'
    case "$OS" in
      linux) sudo journalctl -u ourway-agent --no-pager -n 40 2>/dev/null | sed 's/^/    /' || true ;;
      darwin) sudo tail -40 /tmp/ourway-smoke-agent.log 2>/dev/null | sed 's/^/    /' || true ;;
    esac
  fi
  $SUDO "$AGENT" --uninstall --yes >/dev/null 2>&1 || true
  fi
fi

echo "========================================"
echo "  passed: $PASS   failed: $FAIL"
echo "========================================"
[ "$FAIL" = "0" ] || exit 1
