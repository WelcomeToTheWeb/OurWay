#!/usr/bin/env python3
"""Author and apply the Hindsight memory-bank missions for the OurWay repo.

The coding-agent plugin seeds generic "coding missions" into a bank that has none of its
own, and afterwards it never overwrites what the bank already says. So: write these
missions onto the bank once, and they stick — the plugin's own wording loses.

Missions steer three separate stages of the memory pipeline:

  retain_mission        what gets extracted from a commit, diff, document or chat
  observations_mission  what gets consolidated into durable observations
  reflect_mission       how a question is answered from those memories

Usage
  python3 scripts/hindsight-missions.py            # print the missions as JSON
  python3 scripts/hindsight-missions.py --check    # show live bank config vs these
  python3 scripts/hindsight-missions.py --apply    # create bank + write the missions
  python3 scripts/hindsight-missions.py --dump f   # write the JSON to file f

Server and token come from ~/.hindsight/coding-agent.json (apiUrl, apiToken),
overridable with HINDSIGHT_API_URL / HINDSIGHT_API_TOKEN.
"""

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

BANK_ID = "coding-agent::OurWay"

MISSION = (
    "OurWay RMM: open-source, self-hosted remote monitoring, patching and remote control for "
    "Windows, Linux and macOS fleets at MSP scale. Memory exists to make the next session faster "
    "and more accurate than this one: decide from what was already decided, and say plainly when "
    "nothing in the record covers the question."
)

RETAIN_MISSION = (
    "You are ingesting a commit, commit diff, document, or developer conversation into OurWay's memory. "
    "Extract the FEWEST durable facts that capture what is now true, each bound to the concrete entity it constrains: "
    "a Go file and function (server/api/handlers.go, server/ws/hub.go, server/patching/policy.go, agent/client/client.go, agent/collector/collector.go), "
    "a REST route, a WebSocket message `type` string and its `{type, payload}` envelope, a DB table/column, a model struct field with its `gorm:`/`json:` tag, "
    "an env var (SERVER_PORT, OURWAY_SERVER, OURWAY_DEVICE_KEY, JWT_SECRET, REDIS_*), a CLI/installer flag (--server, --key, --register, --install-dir, --skip-service, --version, --yes), "
    "a web file under web/src (page, component, store, api module, i18n key), or a numeric constant (heartbeat 15s, metrics 60s, streaming 2s, 60s read deadline, 5-min gateway reaper, 10,000-row metric query cap). "
    "Quote identifiers, literals, ports, timeouts, status codes, GOOS/GOARCH pairs (windows/amd64, darwin/arm64) and status strings VERBATIM — never generalise them. "
    "Put the WHY inside the same fact as the WHAT; do not write a separate essay. "
    "Label each fact as a decision, an observed defect, or an unimplemented feature, and keep a review item's severity symbol (🔴/🟠/🟡/🟢/🧩/✅) together with its `file::func` location. "
    "Skip command output, praise, narration, and anything a later session would not reuse."
)

OBSERVATIONS_MISSION = (
    "Consolidate durable knowledge about the OurWay codebase — module responsibilities, the wire contract between agent, server and web, the RBAC rules, the patch pipeline, the installer/update chain, and how a change is verified — "
    "from the ingested commits, diffs, documents and conversations. Keep the map current to the tree: "
    "server/ (main.go, api/ incl. handlers.go, auth.go, sessions.go, reboot.go, sso.go; ws/hub.go; store/; models/; auth/; alerts/; patching/ incl. policy.go, deploy.go, rollback.go, deployment_results.go; sessions/; files/; webhooks/; sso/; cache/; events/; metrics/; config/), "
    "agent/ (main.go, client/, collector/, patch/, session/, files/, install/, selfupdate/), "
    "cmd/ (ourway-cli, ourway-installer, ourway-viewer), web/src/ (pages/, components/, api/, stores/, hooks/, i18n/locales/{en,de,fr}.json, utils/, auth/), docker/, docs/. "
    "Record the project's own conventions as observations: commit subjects are conventional commits (`fix:`, `feat:`, `ci:`, `docs:`, `style:`, optional scope, imperative, no trailing period) landing on `vibe/*` feature branches; "
    "the verification gate is `go -C server build ./... && go -C server vet ./... && go -C server test ./...`, the same three for agent plus `GOOS=windows GOARCH=amd64` and `GOOS=darwin GOARCH=arm64` agent builds, and `npm ci --prefix web && npm run build --prefix web` (CI: .github/workflows/build.yml, `tests` job); "
    "server layering is api → store interface → implementations, routes registered in server/api/handlers.go, and an interface change cascades to the mocks in the same change; "
    "WebSocket messages cross web/server/agent as a `{type, payload}` envelope kept identical on all three sides; "
    "user-facing strings go through `t(...)` with keys introduced in lockstep across en, de, fr, and locals are never named `t`; "
    "when a response shape changes, the tests and the web decoders change in the same commit. "
    "Keep SHIPPED and PLANNED apart: a `docs/roadmap-2.0.md` line marked ⬜ and a buglist.md item marked 🧩 are not implemented. "
    "Favour stable structural understanding, named trap families, and verification rules over one-off detail. "
    "When a new fact contradicts an existing observation, UPDATE it to the current state and note that the rule was revised and when, so the old version reads as history rather than as a competing claim."
)

REFLECT_MISSION = (
    "You are OurWay's debugging and design assistant with this project's past decisions in memory (commit rationale, developer chats, and the graded review in buglist.md). "
    "Given a symptom, find the past decision whose rationale explains the ROOT CAUSE — not one that merely shares vocabulary. "
    "Answer with the PRECISE fix: the EXACT rule and the LITERAL values, identifiers, file paths, routes, message type strings, env vars, ports, timeouts, status codes and GOOS/GOARCH pairs that were decided — quote them VERBATIM, never paraphrase or generalise (give the decided value, not 'the project standard'). "
    "OurWay symptoms cluster in known trap families; search memory for the family the symptom belongs to: "
    "(1) ports and proxies — the web dev server proxies /api and /ws to http://localhost:9090, so the server must run with SERVER_PORT=:9090; the agent defaults to --server http://localhost:8080; production publishes the web on :3000, and the native viewer must dial the port the web proxy publishes rather than the backend's own; "
    "(2) the WebSocket hub — a `{type, payload}` change is a three-sided change (web, server, agent); a 60s read deadline disconnects a device whose heartbeats stall behind inline DB writes; the newest connection wins per device key, so an older one feeds stale metrics and then falsely marks the device offline; "
    "(3) Windows session and launch — never launch the remote exe into Session 0; service context versus interactive desktop; agent, remote exe and viewer must be installed, updated and uninstalled as a checksum-verified set; "
    "(4) installers and enrollment — stale agent download from GitHub, installer lists that hide the viewer/remote exe, apt versus dnf patching differences, per-installation enrollment token required at registration, Register returning 403 once any user exists, uninstall leaving files the operator added; "
    "(5) patching — the policy engine runs hourly in server/patching/policy.go, scope resolved by ScopeValue against device IDs, names or hostnames (comma-separated), maintenance windows, MaxDevicesPerBatch batching, queued offline deploys, reboot-pending, empty deploys returning ErrNoApprovedUpdates, compliance computed as compliant/devices; "
    "(6) roles — session start/input/quality/end require admin/manager/technician, settings and SSO are admin-only, and a list route without a role filter leaks every user's sessions; "
    "(7) i18n and shape drift — a locale key missing from en, de, or fr is a bug, and a changed response shape is only done when the tests and web decoders changed with it. "
    "When memories CONFLICT on one rule, the LATEST decision wins — prefer facts that explicitly amend or supersede earlier ones, state the superseded rule as no longer in effect, and never offer it as the fix. "
    "buglist.md marks items ✅ 'fixed and verified' as of its review date: treat that as a claim to re-check against the current tree, not as present truth. "
    "Name the file and function to change and cite the memory id(s). "
    "If NOTHING in memory genuinely explains THIS symptom, say exactly that in one short sentence — a wrong-but-confident nearest match is worse than an honest miss."
)

MISSIONS = {
    "mission": MISSION,
    "reflect_mission": REFLECT_MISSION,
    "retain_mission": RETAIN_MISSION,
    "observations_mission": OBSERVATIONS_MISSION,
}


def config():
    path = Path(os.environ.get("HINDSIGHT_CONFIG", Path.home() / ".hindsight/coding-agent.json"))
    cfg = {}
    if path.exists():
        cfg = json.loads(path.read_text())
    url = os.environ.get("HINDSIGHT_API_URL") or cfg.get("apiUrl") or "https://api.hindsight.vectorize.io"
    token = os.environ.get("HINDSIGHT_API_TOKEN") or cfg.get("apiToken")
    return url.rstrip("/"), token


def request(method, path, body=None, timeout=60):
    base, token = config()
    req = urllib.request.Request(
        f"{base}/v1/default/banks{path}",
        data=None if body is None else json.dumps(body).encode(),
        method=method,
        headers={"Content-Type": "application/json"}
        | ({"Authorization": f"Bearer {token}"} if token else {}),
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except json.JSONDecodeError:
            return e.code, {"detail": raw}
    except OSError as e:
        return 0, {"detail": str(e)}


def main():
    args = sys.argv[1:]
    bank = urllib.parse.quote(BANK_ID, safe="")

    if not args or args[0] == "--dump":
        out = {"bank_id": BANK_ID} | MISSIONS
        if len(args) > 1:
            Path(args[1]).write_text(json.dumps(out, indent=2) + "\n")
            print(f"wrote {args[1]}")
        else:
            print(json.dumps(out, indent=2))
        return 0

    if args[0] == "--check":
        status, data = request("GET", f"/{bank}/config")
        if status != 200:
            print(f"GET config failed: {status} {data.get('detail')}")
            return 1
        live = data.get("config", {})
        for key, want in MISSIONS.items():
            if key == "mission":
                continue
            got = live.get(key)
            print(f"{'OK  ' if got == want else 'DRIFT'} {key} ({len(want)} chars authored)")
        return 0

    if args[0] == "--apply":
        status, data = request("PUT", f"/{bank}", MISSIONS)
        print(f"create/update bank: {status} {str(data)[:200]}")
        if status >= 400:
            return 1
        status, data = request("PATCH", f"/{bank}/config", {"updates": {k: v for k, v in MISSIONS.items() if k != "mission"}})
        print(f"config patch: {status} {str(data)[:200]}")
        return 0 if status < 400 else 1

    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main())
