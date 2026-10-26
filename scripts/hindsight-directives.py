#!/usr/bin/env python3
"""OurWay Hindsight directives.

Directives are the hard rules Hindsight injects into every reflect prompt
(`POST /v1/default/banks/{bank}/directives`). They are short by design: the
long-form guidance lives in the bank's missions (see hindsight-missions.py);
a directive is a rule a synthesis must not violate.

Idempotent: matches on name, updates content/priority in place, creates what
is missing. Run:

    python3 scripts/hindsight-directives.py            # apply
    python3 scripts/hindsight-directives.py --check    # report only
"""

import json
import sys
import urllib.parse
import urllib.request

BASE = "http://10.0.23.5:8888"
BANK = "coding-agent::OurWay"

# tags: [] = global (always injected). Non-empty tags only apply to a reflect
# run whose scope matches those tags, so scoped directives are for narrow,
# source-specific rules and global is the safe default.
DIRECTIVES = [
    {
        "name": "latest-decision-wins",
        "priority": 100,
        "tags": [],
        "content": (
            "When memories conflict about one rule, the LATEST decision wins. State the superseded "
            "rule as no longer in effect and never propose it as the fix. Date the answer: say which "
            "memory carries the current rule and which one is history."
        ),
    },
    {
        "name": "quote-literals-name-the-place",
        "priority": 95,
        "tags": [],
        "content": (
            "Answer with literals, never with a generalisation: the file::func to change, and the exact "
            "port, interval, timeout, status code, route, WebSocket message `type` string, env var, "
            "i18n key, GOOS/GOARCH pair that the decision fixed. 'The project standard' is not an answer; "
            "the value is."
        ),
    },
    {
        "name": "memory-is-a-past-record",
        "priority": 90,
        "tags": [],
        "content": (
            "Memory says what was true when it was written, including 'fixed', 'verified', 'passes' and "
            "buglist ✅ items. Present those as claims to check against the current tree, not as present "
            "state. Say plainly when nothing in memory covers the question — a nearest-match answer is "
            "worse than an honest miss."
        ),
    },
    {
        "name": "three-sided-change-rule",
        "priority": 85,
        "tags": [],
        "content": (
            "Any WebSocket message shape, route shape, or device-facing field is a three-sided change: "
            "name the web call site (web/src/api, stores, hooks), the server handler (server/api) and the "
            "agent site (agent/client, agent/collector) it touches. A response-shape answer is incomplete "
            "unless it includes the tests and the web decoders that must change in the same commit."
        ),
    },
    {
        "name": "done-means-the-gate",
        "priority": 80,
        "tags": [],
        "content": (
            "A change is done only when the project gate passes: server build/vet/test, agent "
            "build/vet/test, agent cross-builds windows/amd64 and darwin/arm64, and the web typecheck + "
            "build. User-facing strings go through t(...) with the key added to en, de AND fr — a key "
            "missing from any locale is a defect, not a polish item."
        ),
    },
    {
        "name": "claim-attribution",
        "priority": 75,
        "tags": [],
        "content": (
            "In a conversation, the user's statements are requirements and decisions; the agent's own "
            "'fixed it', 'passes', 'verified' lines are the agent's claims. Keep them attributed. Never "
            "turn an agent's claim into a project rule, and never silently merge the two."
        ),
    },
    {
        "name": "commit-diffs-carry-the-why",
        "priority": 70,
        "tags": ["source:git", "source:gitlog"],
        "content": (
            "A commit subject is the 'what'; the diff is the 'why'. Record the literal values the diff "
            "moved (port, interval, status code, message type, default, GOOS/GOARCH) and the trap it closed. "
            "Conventional-commit scope in the subject (`fix(ws): …`) is part of the fact."
        ),
    },
    {
        "name": "documents-are-contracts-git-is-state",
        "priority": 65,
        "tags": ["source:upload", "source:document"],
        "content": (
            "Ingested documents state intent, contracts and constraints (rate limits, env-var forms, "
            "design rules, gate expectations). When a document and a commit disagree about how code "
            "behaves today, the code is the state and the document is the contract: name the gap, do not "
            "average it."
        ),
    },
    {
        "name": "known-trap-families",
        "priority": 60,
        "tags": [],
        "content": (
            "Classify the symptom into a trap family before answering, and search memory for that family: "
            "ports and proxies; the WebSocket hub (origin allow-list form, read deadline, "
            "newest-connection-wins per device key); Windows session and launch context; installers, "
            "enrollment and re-registration; the patch pipeline (policy scope, windows, batching, "
            "reboot-pending, empty deploys); RBAC on routes (a list route without a role filter); request "
            "rate limits and polling fan-out; i18n and response-shape drift. Name the family in the answer."
        ),
    },
]


def request(method, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(
        f"{BASE}{path}",
        data=data,
        method=method,
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=60) as resp:
        return resp.status, json.loads(resp.read() or b"{}")


def main():
    check = "--check" in sys.argv
    quoted = urllib.parse.quote(BANK, safe="")
    status, existing = request("GET", f"/v1/default/banks/{quoted}/directives")
    by_name = {d["name"]: d for d in existing.get("items", [])}

    for d in DIRECTIVES:
        cur = by_name.get(d["name"])
        payload = {
            "name": d["name"],
            "content": d["content"],
            "priority": d["priority"],
            "is_active": True,
            "tags": d["tags"],
        }
        if cur:
            same = cur.get("content") == payload["content"] and cur.get("priority") == payload["priority"]
            print(f"{'OK' if same else 'UP'}   {d['name']} (p{d['priority']}, tags={d['tags']})")
            if not same and not check:
                request("PATCH", f"/v1/default/banks/{quoted}/directives/{cur['id']}", payload)
        else:
            print(f"NEW  {d['name']} (p{d['priority']}, tags={d['tags']})")
            if not check:
                request("POST", f"/v1/default/banks/{quoted}/directives", payload)


if __name__ == "__main__":
    main()
