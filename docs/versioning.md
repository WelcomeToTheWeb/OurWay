# OurWay version scheme

One rule: **the git tag is the version**. Every artifact — server binary, web
bundle, agent, installer, viewer, Docker image, GitHub release — carries the
same string, stamped at build time from the tag. Nothing else is a source of
version.

## Release versions

```
vMAJOR.MINOR.PATCH            e.g. v2.1.0, v2.1.3
```

Semantic versioning, with the meaning pinned to what an operator can rely on:

| Bump | When | Operator consequence |
|---|---|---|
| **MAJOR** | Breaking change to the wire contract (`/api/**` response shapes, the WebSocket `{type, payload}` message types), the database schema, the agent↔server pairing, or the install/upgrade path. Migration guide required. | Stop and read `docs/migration-guide.md` before upgrading. |
| **MINOR** | New capability, backwards compatible: new routes, new message types, new web surfaces, new env vars. Agents keep working; older agents keep working. | Upgrade at your convenience. |
| **PATCH** | A fix, backwards compatible with everything: a bug, a security fix, a docs/build fix. | Upgrade promptly, especially security patches. |

`v2.0.3` is the last released version; the scheme applies from the next release.

Rules that keep the numbers meaningful:

1. **No version is invented before the work exists.** The tag is cut from the
   commit that finishes the scope, not planned ahead.
2. **A version is never re-used.** Once pushed, a tag is frozen; a bad release
   is superseded by the next patch (`v2.1.1`), not moved.
3. **A release contains only what passed the gate**, on the tag commit
   (`.github/workflows/build.yml` → `tests` job).
4. **Wire-contract changes raise MINOR at minimum** — a new message type is a
   new capability agents must understand. A shape change that breaks existing
   agents is MAJOR.
5. **Security fixes are PATCH** and ship without bundling features, so an
   operator can take the fix without taking the change.

## Channels

| Channel | Where it comes from | Image tags | Agents auto-update to it? |
|---|---|---|---|
| **stable** | a `vX.Y.Z` tag | `vX.Y.Z`, `X.Y`, `latest` | **yes** (default) |
| **edge** | `main` | `edge`, `main-<sha>` | only if `OURWAY_UPDATE_CHANNEL=edge` |
| **dev** | a local unstamped build (`dev`) | none (never pushed) | never |

- `latest` is **only ever a stable release**, and only moves to a stable
  release. This is what `docker-compose.prod.yml` pins.
- Image tags keep the `v` prefix of the git tag (`v2.1.0`), matching the
  `v1.5.x` and `v2.0.x` tags already in `ghcr.io/welcometotheweb/ourway/server`.
  The floating minor does not: it reads `2.1`, Docker-style.
- `X.Y` is a floating minor: it tracks the newest stable patch of that minor
  (`v2.1.3` moves `2.1`). Pin `X.Y.Z` if you want to be immune to that.
- `edge` carries a prerelease string, e.g. `2.1.0-dev.20261026+5f03d77`
  (prerelease = build date, build metadata = short commit). It is never
  `latest`, never `X.Y`, and never offered to a stable agent.
- `dev` means "unstamped local build" (`server/config.Version = "dev"`), and
  self-update ignores it: it carries no ordering.

## How the string gets into a binary

Stamped with `-ldflags`, never hand-edited:

| Artifact | Variable | Value on a tag | Value on `main` |
|---|---|---|---|
| server | `ourway/server/config.Version` | `2.1.0` | `2.1.0-dev.20261026+5f03d77` |
| server | `ourway/server/config.GitCommit` | short sha | short sha |
| agent | `ourway/agent/config.Version` | `2.1.0` | `2.1.0-dev.20261026+5f03d77` |
| installer | `main.version` | `2.1.0` | same rule |
| viewer | — | ships as a stable artifact | ships as a stable artifact |
| web | `VITE_APP_VERSION` | `2.1.0` | same rule |

`.github/workflows/build.yml` computes `VERSION` from `GITHUB_REF` on a tag,
and from `main`'s next-minor dev string otherwise. On a tag it uses the exact
tag: `git describe` can race the tag push and emit a `vX.Y.Z-N-gSHA`
pseudo-version, which the agent's self-update cannot order.

## Self-update follows the scheme, not "is it different"

An agent takes an update only when the server's version **orders above** its
own under this scheme:

1. Parse both sides as semver. A side that does not parse (`dev`, a raw sha, an
   empty string) means **no update** — an unstamped build must never chase a
   build stamp.
2. Update when `server > local` by major → minor → patch → prerelease order.
   Equal, or lower, means **no update** (a downgrade is never automatic).
3. A stable agent ignores a prerelease server version entirely. Set
   `OURWAY_UPDATE_CHANNEL=edge` on both sides to follow `main`.

Why the ordering rule, not equality: a server stamped per-commit on `main`
makes "different" true on every push, which would re-update every agent in the
fleet dozens of times a day, with no ordering and no way to say which is
newer. Ordering plus channel makes auto-update a channel policy, not a race.

## Cutting a release

```sh
scripts/release.sh 2.1.0            # patch/minor/major: the number you choose
```

What it does, in order:

1. Refuse a dirty tree, a non-`main` branch, a missing upstream, an existing
   tag, and a number that is not greater than the latest tag.
2. Run the full gate (server, agent, cli, installer, viewer, web).
3. Stamp `web/package.json` with the version.
4. Commit `chore(release): 2.1.0`, tag `v2.1.0`, push branch **and** tag.
5. The CI's `release` job publishes images, binaries, installers and the
   GitHub release notes; the agent install scripts fetch assets from
   `releases/download/vX.Y.Z/`, so the tag is the release contract.

`scripts/release.sh --check 2.1.0` runs every check and the gate and publishes
nothing. `scripts/release.sh --dry-run 2.1.0` shows what it would do.

## Where version shows up

- `GET /api/agent/version` → `{"version": "2.1.0", "git_commit": "5f03d77"}`
  (no auth; drives agent self-update and the console's "server is …" line).
- `ourway-agent --version`, `ourway-installer --version` print the stamped
  string, so an enrolled unit reports the fleet's version exactly.
- Image labels: `org.opencontainers.image.version` and
  `org.opencontainers.image.source` on every published image.
- `docs/changelog.md` keeps one section per `vX.Y.Z`, in release order.
