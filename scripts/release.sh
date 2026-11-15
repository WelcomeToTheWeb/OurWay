#!/usr/bin/env bash
# Cut an OurWay release: verify the gate, stamp the version, tag, push.
#
#   scripts/release.sh 2.1.0            cut and publish v2.1.0
#   scripts/release.sh --check 2.1.0    run every check and the gate, publish nothing
#   scripts/release.sh --dry-run 2.1.0  show the checks and what would be published
#   scripts/release.sh --skip-gate 2.1.0  cut without running the gate here: the
#                                CI tests job runs the identical gate on the
#                                tag and blocks publishing if it is red. Use
#                                only where the toolchain is not installed.
#   scripts/release.sh --from release/2.3.x --maintenance 2.3.1
#                                cut a maintenance PATCH from a release line
#                                other than main. --maintenance is required
#                                when that line is older than the newest
#                                published release, because :latest must not
#                                move backwards - see step 3 below.
#
# The git tag is the version: see docs/versioning.md.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

CHECK_ONLY=0
DRY_RUN=0
SKIP_GATE=0
MAINTENANCE=0
VERSION=""
FROM_BRANCH="main"

fail() {
  echo "release refused: $*" >&2
  exit 1
}

step() { echo "▶ $*"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --check) CHECK_ONLY=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --skip-gate) SKIP_GATE=1 ;;
    --maintenance) MAINTENANCE=1 ;;
    --from)
      [[ $# -ge 2 ]] || fail "--from needs a branch name"
      FROM_BRANCH="$2"
      shift
      ;;
    --from=*) FROM_BRANCH="${1#--from=}" ;;
    -h | --help)
      awk 'NR == 1 { next } /^set -euo/ { exit } { sub(/^# ?/, ""); print }' "$0"
      exit 0
      ;;
    -*) fail "unknown flag: $1" ;;
    *) VERSION="$1" ;;
  esac
  shift
done

if [[ -z "$VERSION" ]]; then
  echo "usage: scripts/release.sh [--check|--dry-run|--skip-gate] [--from <branch>] [--maintenance] X.Y.Z" >&2
  exit 2
fi
# --- 1. the tree is releasable, on the branch being released ------------------
# Before the version maths: the `git tag --merged` below is only worth
# something once origin/<branch> and the tags have been fetched.
[[ -z "$(git status --porcelain)" ]] || fail "working tree is dirty; commit or stash first"

[[ "$(git branch --show-current)" == "$FROM_BRANCH" ]] ||
  fail "releases are cut from $FROM_BRANCH, not $(git branch --show-current)"

git rev-parse --verify "origin/$FROM_BRANCH" >/dev/null 2>&1 || fail "origin/$FROM_BRANCH not fetched"
git fetch --quiet origin "$FROM_BRANCH"
git fetch --quiet --tags origin || true   # the tags are what step 2 reads
[[ "$(git rev-parse HEAD)" == "$(git rev-parse "origin/$FROM_BRANCH")" ]] ||
  fail "HEAD is not the pushed tip of origin/$FROM_BRANCH; push the commits first"

git cat-file -e "HEAD:docker/Dockerfile.server" 2>/dev/null || fail "not the OurWay repository root"

# --- 2. the number is a sane semver, ahead of THIS LINE -----------------------
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "$VERSION is not X.Y.Z (no leading v)"
git rev-parse -q --verify "refs/tags/v${VERSION}" >/dev/null && fail "tag v${VERSION} already exists"

# The newest tag reachable from the branch being released, not the newest tag
# in the repository. A maintenance PATCH on an older line is legitimately
# behind main's newest release, and comparing against that would refuse the
# exact release docs/versioning.md rule 5 asks for: a security fix ships as a
# PATCH, on its own, so an operator can take it without taking the features.
LATEST_TAG=$(git tag --merged "origin/$FROM_BRANCH" --list 'v[0-9]*' --sort=-v:refname | head -1)
if [[ -n "$LATEST_TAG" ]]; then
  step "newest tag on $FROM_BRANCH: $LATEST_TAG"
  # sort -V puts the OLDER of the two first: if the new number comes out
  # first, it is not strictly greater than the released one.
  if [[ "$(printf '%s\n%s\n' "$VERSION" "${LATEST_TAG#v}" | sort -V | head -1)" == "$VERSION" ]]; then
    fail "$VERSION is not greater than ${LATEST_TAG#v}"
  fi
fi

# --- 3. an older line must not steal :latest ----------------------------------
# Any v* tag push publishes :latest, and :latest is what docker-compose.prod.yml
# pins and what stable agents auto-update to. Pointing it at an older release
# would downgrade every install that follows it, so cutting a PATCH on an older
# line takes an explicit --maintenance, and build.yml applies the same rule from
# the tag itself rather than trusting the operator to remember it.
LINE=$(printf '%s' "$VERSION" | cut -d. -f1,2)
NEWEST_TAG=$(git tag --list 'v[0-9]*' --sort=-v:refname | head -1)
if [[ -n "$NEWEST_TAG" ]]; then
  NEWEST_LINE=$(printf '%s' "${NEWEST_TAG#v}" | cut -d. -f1,2)
  if [[ "$(printf '%s\n%s\n' "$LINE" "$NEWEST_LINE" | sort -V | tail -1)" != "$LINE" ]]; then
    if [[ "$MAINTENANCE" != 1 ]]; then
      fail "v$VERSION is on the $LINE line, but $NEWEST_TAG is already published: :latest and stable agents would move BACKWARDS. Re-run with --maintenance (then vX.Y.Z and $LINE are published and :latest is left alone)."
    fi
    step "maintenance release: v$VERSION on the $LINE line; newest published is $NEWEST_TAG"
    echo "  publishing v$VERSION and $LINE only; :latest stays on $NEWEST_TAG."
    echo "  afterwards confirm ghcr.io/welcometotheweb/ourway/server:latest still"
    echo "  resolves to $NEWEST_TAG - that is the tag docker-compose.prod.yml pins."
  fi
fi

# --- 4. the gate --------------------------------------------------------------
if [[ "$SKIP_GATE" == "1" ]]; then
  step "gate skipped (--skip-gate): CI runs the identical gate on the tag and blocks publishing if red"
else
step "gate: server"
go -C server build ./...
go -C server vet ./...
go -C server test ./...

step "gate: agent"
go -C agent build ./...
go -C agent vet ./...
go -C agent test ./...
GOOS=windows GOARCH=amd64 go -C agent build -o /dev/null ./...
GOOS=darwin GOARCH=arm64 go -C agent build -o /dev/null ./...

step "gate: cli, installer, viewer"
go -C cmd/ourway-cli build ./...
go -C cmd/ourway-installer build ./...
go -C cmd/ourway-viewer vet ./...
go -C cmd/ourway-viewer test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go -C cmd/ourway-viewer build -o /dev/null ./...

step "gate: web"
npm ci --prefix web
npm test --prefix web -- --run
npm run build --prefix web

step "gate: 2.2 checks"
# The floors 2.2 added, in the order the plan lists them: coverage may not
# drift down, the three locales may not drift apart, and the console may not
# confirm with a browser dialog (DESIGN.md §6).
scripts/coverage.sh
python3 scripts/i18n-parity.py
scripts/browser-dialogs-check.sh
fi

if [[ "$CHECK_ONLY" == "1" ]]; then
  echo
  echo "all checks passed for v${VERSION}; nothing published (--check publishes nothing)"
  exit 0
fi

# --- 5. stamp the one file that carries a version ----------------------------
step "stamp web/package.json -> $VERSION"
if [[ "$DRY_RUN" == "1" ]]; then
  echo "  (dry run) package.json version -> $VERSION"
else
  node -e '
    const fs = require("fs");
    const p = "web/package.json";
    const pkg = JSON.parse(fs.readFileSync(p, "utf8"));
    pkg.version = process.argv[1];
    fs.writeFileSync(p, JSON.stringify(pkg, null, 2) + "\n");
  ' "$VERSION"
fi

# --- 6. commit, tag, push ----------------------------------------------------
if [[ "$DRY_RUN" == "1" ]]; then
  echo
  echo "dry run: would commit 'chore(release): $VERSION', tag v${VERSION},"
  echo "         push $FROM_BRANCH and the tag; CI publishes images and the release."
  git status --short
  exit 0
fi

step "commit and tag"
git add web/package.json
git commit -m "chore(release): $VERSION"
git tag -a "v${VERSION}" -m "OurWay $VERSION"

step "push branch and tag (CI publishes images, binaries and the release)"
git push origin "$FROM_BRANCH"
git push origin "v${VERSION}"

echo
echo "released v${VERSION}: watch Actions for the Build & Release workflow"
if [[ "$MAINTENANCE" == 1 ]]; then
  echo "  images   ghcr.io/welcometotheweb/ourway/{server,web}:v${VERSION} + :${VERSION%.*}  (:latest is NOT republished: $NEWEST_TAG stays newest)"
else
  echo "  images   ghcr.io/welcometotheweb/ourway/{server,web}:v${VERSION} + :${VERSION%.*} + :latest"
fi
echo "  release  https://github.com/WelcomeToTheWeb/OurWay/releases/tag/v${VERSION}"
