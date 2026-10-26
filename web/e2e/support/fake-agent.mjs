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
 * lookup are exercised for real.
 *
 * Usage: node fake-agent.mjs --ws ws://host:port/ws --keys key1,key2 [--label unit]
 */
import { parseArgs } from 'node:util';

const { values: args } = parseArgs({
  options: {
    ws: { type: 'string' },
    keys: { type: 'string' },
    label: { type: 'string', default: 'fake-agent' },
  },
});

const wsURL = args.ws;
const keys = (args.keys ?? '').split(',').map((k) => k.trim()).filter(Boolean);

if (!wsURL || keys.length === 0) {
  console.error('fake-agent: --ws <url> and --keys <k1,k2> are required');
  process.exit(2);
}

const sockets = [];
let stopping = false;

function connect(key, attempt = 0) {
  if (stopping) return;
  const ws = new WebSocket(wsURL, ['ourway-auth', key]);
  sockets.push(ws);

  ws.onopen = () => {
    console.log(`${args.label}: connected`);
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
  ws.onclose = () => {
    const index = sockets.indexOf(ws);
    if (index >= 0) sockets.splice(index, 1);
    if (stopping) return;
    // Reconnect with the same discipline an agent uses: back off, do not swarm.
    const delay = Math.min(30_000, 500 * 2 ** attempt);
    setTimeout(() => connect(key, attempt + 1), delay);
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
