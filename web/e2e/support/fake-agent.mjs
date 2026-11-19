#!/usr/bin/env node
/**
 * 2.2-0 — fake agent (e2e support).
 *
 * Holds the device WebSocket open for seeded units, the way an installed agent
 * does: `Sec-WebSocket-Protocol: ourway-auth, <device key>` against `/ws`.
 *
 * Why this exists: a browser page cannot stand in for an agent. The page's
 * connection dies on navigation, so a remote session started from the console
 * would have no peer and the ledger would show nothing. Sessions, frame
 * forwarding and the presence chip all need a device that stays connected
 * across the run.
 *
 * It is deliberately a *client*, not a mock: it speaks the product's own
 * handshake, so the hub's origin check, subprotocol negotiation and credential
 * lookup are exercised for real. It also sends the two messages an installed
 * agent repeats — `heartbeat` and `metrics` — in the agent's own envelope
 * (`{type, payload}`, see agent/client/client.go), carrying the unit's seeded
 * load and its self-reported remote-control state (2.2-1).
 *
 * Usage: node fake-agent.mjs --ws ws://host:port/ws --keys key1,key2 \
 *        [--report '[{"deviceKey":"…","inputCapability":"denied","captureState":"error","data":{…}}]']
 *        [--label unit]
 */
import { parseArgs } from 'node:util';

const { values: args } = parseArgs({
  options: {
    ws: { type: 'string' },
    keys: { type: 'string' },
    report: { type: 'string' },
    label: { type: 'string', default: 'fake-agent' },
  },
});

const wsURL = args.ws;
const keys = (args.keys ?? '').split(',').map((k) => k.trim()).filter(Boolean);

if (!wsURL || keys.length === 0) {
  console.error('fake-agent: --ws <url> and --keys <k1,k2> are required');
  process.exit(2);
}

// --report is a JSON array built by global setup. The seeded load travels with
// it (a 95 % CPU unit, a 97 % disk unit), so the alert engine keeps evaluating
// the fleet the seeder described — a constant "healthy" sample from the wire
// would quietly unmake the alert tests a few seconds into the run.
const reports = new Map();
try {
  for (const entry of JSON.parse(args.report ?? '[]')) {
    reports.set(entry.deviceKey, {
      inputCapability: entry.inputCapability || 'unknown',
      captureState: entry.captureState || 'unknown',
      data: entry.data || {},
    });
  }
} catch (err) {
  console.error(`fake-agent: --report is not valid JSON: ${err.message}`);
}

const sockets = [];
// One pending reconnect per device key: a key with two sockets in flight has two
// sockets taking turns displacing each other, and the hub keeps exactly one.
const pending = new Map();
let stopping = false;

/**
 * Heartbeat + metrics, on connect and every few seconds. The capability fields
 * are the 2.2-1 contract: what the unit can do in a remote session, stated by
 * the unit, stored by the server, printed by the console.
 */
function reportLoop(key, ws) {
  const report = reports.get(key) ?? { inputCapability: 'unknown', captureState: 'unknown', data: {} };
  const tick = () => {
    if (ws.readyState !== WebSocket.OPEN) return;
    const now = Math.floor(Date.now() / 1000);
    ws.send(JSON.stringify({ type: 'heartbeat', payload: { device_key: key, timestamp: now } }));
    ws.send(JSON.stringify({
      type: 'metrics',
      payload: {
        device_key: key,
        hostname: key.slice(0, 8),
        timestamp: now,
        input_capability: report.inputCapability,
        capture_state: report.captureState,
        data: report.data,
      },
    }));
  };
  tick();
  setInterval(tick, 4000).unref?.();
}

/**
 * Frames, while a session is live.
 *
 * This is the part of the capture loop an installed agent runs and this harness
 * used not to. `POST /api/devices/:id/sessions` creates a session as `pending`,
 * and the first frame is the only thing in the product that makes it `active`
 * (`server/api/sessions.go:347-358`; the WebSocket relay does the same at
 * `server/ws/hub.go:866-879`). A device that answers `session_start` with silence
 * therefore leaves every session pending forever — which is how
 * `sessions.spec.ts` came to assert `['pending','active']` and pass: the
 * disjunction was always the left-hand side, and a session that never receives a
 * frame is indistinguishable from one that is working.
 *
 * The upload is the agent's own legacy path — `X-Device-Key`, the frame endpoint,
 * and the `server_url` from the `session_start` payload — because that is what
 * `agent/session/session.go:303-333` does. Inventing a shorter route here would
 * hide the one thing a harness like this is for: proving the URL and the
 * credential we hand a device actually reach the endpoint.
 *
 * A unit that reports `capture_state: error` sends nothing. Its reported state is
 * the state this harness must be consistent with: a device whose screen cannot be
 * captured does not produce frames, and the console's "Capture failing" badge
 * would otherwise be contradicted by the same wire that set it.
 */
const streaming = new Map();

function startStreaming(key, payload) {
  const report = reports.get(key);
  if (report && report.captureState !== 'ok') {
    console.log(`${args.label}: session_start for ${key.slice(0, 8)} — reporting capture error, sending no frames`);
    return;
  }
  const sessionID = payload?.session_id;
  const serverURL = payload?.server_url;
  if (!sessionID || !serverURL) {
    console.log(`${args.label}: session_start without session_id/server_url — cannot upload frames`);
    return;
  }
  stopStreaming(key);

  const url = `${serverURL.replace(/\/$/, '')}/api/sessions/${sessionID}/frame`;
  // SOI then EOI: a degenerate but well-formed JPEG. The server stores
  // and relays the bytes; nothing in this suite decodes them.
  const frame = Buffer.from([0xff, 0xd8, 0xff, 0xd9]);
  let warned = false;
  const tick = async () => {
    try {
      const res = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'image/jpeg', 'x-device-key': key },
        body: frame,
      });
      if (!res.ok && !warned) {
        warned = true;
        console.log(`${args.label}: frame upload returned ${res.status} for ${sessionID.slice(0, 8)}`);
      }
    } catch (err) {
      if (!warned) {
        warned = true;
        console.log(`${args.label}: frame upload failed for ${sessionID.slice(0, 8)}: ${err?.message ?? err}`);
      }
    }
  };
  console.log(`${args.label}: session_start ${sessionID.slice(0, 8)} → streaming frames to ${url}`);
  void tick();
  streaming.set(key, setInterval(() => void tick(), 1_000));
}

function stopStreaming(key) {
  const timer = streaming.get(key);
  if (timer) {
    clearInterval(timer);
    streaming.delete(key);
  }
}

function connect(key, attempt = 0) {
  if (stopping) return;
  const ws = new WebSocket(wsURL, ['ourway-auth', key]);
  sockets.push(ws);
  ws.onopen = () => {
    console.log(`${args.label}: connected`);
    reportLoop(key, ws);
  };
  ws.onmessage = (event) => {
    // Answer the hub's liveness ping and acknowledge session frames, so the
    // device reads as a live agent rather than a half-open socket.
    let msg;
    try {
      msg = JSON.parse(event.data);
    } catch {
      return;
    }
    if (msg?.type === 'ping') {
      ws.send(JSON.stringify({ type: 'pong' }));
    }
    // The agent's capture loop only runs after `session_start` and stops at
    // `session_end` (agent/session/session.go:83). Standing in for both halves is
    // what makes the session lifecycle assertable from the outside.
    if (msg?.type === 'session_start') {
      startStreaming(key, msg.payload);
    }
    if (msg?.type === 'session_end') {
      stopStreaming(key);
    }
  };
  ws.onclose = (event) => {
    const index = sockets.indexOf(ws);
    if (index >= 0) sockets.splice(index, 1);
    // The session died with the socket: an interval left running uploads frames
    // to a session the hub has already ended.
    stopStreaming(key);
    if (stopping) return;
    // Say when a unit drops and why. "the device is not connected" from the
    // session API is the product's answer to a fleet the harness lost track
    // of, and a harness that prints nothing on close cannot tell a
    // displacement from an outage.
    console.log(`${args.label}: closed ${key.slice(0, 8)} code=${event?.code} reason=${event?.reason || 'none'}`);
    // Back off when the network is the problem — a 1006 with no reason is a
    // dropped connection, and swarming a server that is unhappy is how a
    // reconnect becomes an outage. A *displacement* is not that: the hub keeps
    // one connection per device key and closes the older one with
    // StatusPolicyViolation (1008, "superseded by newer connection") — so when
    // a test opens the device socket, the run must be online again in
    // milliseconds, not 500, and not in a 30-second back-off.
    //
    // The CI run named this bug: 1008 was read as an outage, so every
    // displacement put the unit in exponential back-off, the reconnect
    // displaced the socket that displaced it, and the Mac unit sat offline for
    // tens of seconds while its stored status went on saying `online`. The
    // session API answered 409 and the suite timed out on a unit the harness
    // itself had parked.
    const reason = String(event?.reason ?? '');
    const displaced =
      event?.code === 1008 || event?.code === 1000 || event?.code === 4001 || /superseded/i.test(reason);
    const delay = displaced ? 50 : Math.min(30_000, 500 * 2 ** attempt);
    // One pending reconnect per unit, always the freshest one: two sockets for
    // one key means two sockets taking turns displacing each other.
    const queued = pending.get(key);
    if (queued) clearTimeout(queued);
    pending.set(
      key,
      setTimeout(() => {
        pending.delete(key);
        connect(key, displaced ? attempt : attempt + 1);
      }, delay)
    );
  };
  ws.onerror = () => {
    // onclose follows onerror and owns the reconnect.
  };
}

for (const key of keys) connect(key);

function shutdown() {
  stopping = true;
  for (const ws of sockets) {
    try {
      ws.close(1000, 'e2e run finished');
    } catch {
      // already closed
    }
  }
  setTimeout(() => process.exit(0), 200).unref();
}

process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);

console.log(`${args.label}: ${keys.length} device connection(s) → ${wsURL}`);
