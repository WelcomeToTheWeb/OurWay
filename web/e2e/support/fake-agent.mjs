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
  };
  ws.onclose = (event) => {
    const index = sockets.indexOf(ws);
    if (index >= 0) sockets.splice(index, 1);
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
