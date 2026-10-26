#!/usr/bin/env bash
# Cut an OurWay release: verify the gate, stamp the version, tag, push.
#
#   scripts/release.sh 2.1.0            cut and publish v2.1.0
#   scripts/release.sh --check 2.1.0    run every check and the gate, publish nothing
#   scripts/release.sh --dry-run 2.1.0  show the checks and what would be published
#
# The git tag is the version: see docs/versioning.md.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

CHECK_ONLY=0
DRY_RUN=0
VERSION=""

for arg in "$@"; do
  case "$arg" in
    --check) CHECK_ONLY=1 ;;
    --dry-run) DRY_RUN=1 ;;
    -h | --help)
      sed -n '2,10p' "$0"
      exit 0
      ;;
    *) VERSION="$arg" ;;
  esac
done

if [[ -z "$VERSION" ]]; then
  echo "usage: scripts/release.sh [--check|--dry-run] X.Y.Z" >&2
  exit 2
fi

fail() {
  echo "release refused: $*" >&2
  exit 1
}

step() { echo "▶ $*"; }

# --- 1. the number is a sane, greater-than-current semver -------------------
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "$VERSION is not X.Y.Z (no leading v)"

LATEST_TAG=$(git tag --list 'v[0-9]*' --sort=-v:refname | head -1)
if [[ -n "$LATEST_TAG" ]]; then
  step "latest tag: $LATEST_TAG"
  # sort -V puts the OLDER of the two first: if the new number comes out
  # first, it is not strictly greater than the released one.
  if [[ "$(printf '%s\n%s\n' "$VERSION" "${LATEST_TAG#v}" | sort -V | head -1)" == "$VERSION" ]]; then
    fail "$VERSION is not greater than ${LATEST_TAG#v}"
  fi
fi
git rev-parse -q --verify "refs/tags/v${VERSION}" >/dev/null && fail "tag v${VERSION} already exists"

# --- 2. the tree is releasable ------------------------------------------------
[[ -z "$(git status --porcelain)" ]] || fail "working tree is dirty; commit or stash first"

BRANCH=$(git branch --show-current)
[[ "$BRANCH" == "main" ]] || fail "releases are cut from main, on $BRANCH"

git rev-parse --verify "origin/$BRANCH" >/dev/null 2>&1 || fail "origin/$BRANCH not fetched"
git fetch --quiet origin "$BRANCH"
[[ "$(git rev-parse HEAD)" == "$(git rev-parse "origin/$BRANCH")" ]] ||
  fail "HEAD is not the pushed tip of origin/$BRANCH; push the commits first"

git cat-file -e "origin/main:docker/Dockerfile.server" 2>/dev/null || fail "not the OurWay repository root"

# --- 3. the gate --------------------------------------------------------------
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

if [[ "$CHECK_ONLY" == "1" ]]; then
  echo
  echo "all checks passed for v${VERSION}; nothing published (--check publishes nothing)"
  exit 0
fi

# --- 4. stamp the one file that carries a version ----------------------------
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

# --- 5. commit, tag, push ----------------------------------------------------
if [[ "$DRY_RUN" == "1" ]]; then
  echo
  echo "dry run: would commit 'chore(release): $VERSION', tag v${VERSION},"
  echo "         push main and the tag; CI publishes images and the release."
  git status --short
  exit 0
fi

step "commit and tag"
git add web/package.json
git commit -m "chore(release): $VERSION"
git tag -a "v${VERSION}" -m "OurWay $VERSION"

step "push branch and tag (CI publishes images, binaries and the release)"
git push origin "$BRANCH"
git push origin "v${VERSION}"

echo
echo "released v${VERSION}: watch Actions for the Build & Release workflow"
echo "  images   ghcr.io/welcometotheweb/ourway/{server,web}:v${VERSION} + :${VERSION%.*} + :latest"
echo "  release  https://github.com/WelcomeToTheWeb/OurWay/releases/tag/v${VERSION}"
