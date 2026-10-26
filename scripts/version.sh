#!/usr/bin/env bash
# Resolve the version string for a build. The git tag is the version
# (docs/versioning.md); this is the single place that derives it.
#
#   scripts/version.sh            -> version string
#   scripts/version.sh --channel  -> stable | edge
#   scripts/version.sh --commit   -> short sha
#
# On a tag commit: the exact tag (X.Y.Z). git describe is deliberately NOT
# used on a tag push: it can race the tag and emit X.Y.Z-N-gSHA.
# On main: the next patch as a prerelease, e.g. 2.0.4-dev.20261026+5f03d77 —
# ordered above the released patch, and ignored by stable agents.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

MODE="${1:-version}"
COMMIT=$(git rev-parse --short HEAD)

ref="$(git rev-parse --abbrev-ref HEAD)"
tag=""
if [[ "$ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  tag="$ref"                       # checked-out tag (detached)
else
  exact=$(git tag --points-at HEAD --list 'v[0-9]*' | head -1)
  [[ -n "$exact" ]] && tag="${exact#v}"
fi

if [[ -n "$tag" ]]; then
  version="$tag"
  channel="stable"
else
  latest=$(git tag --list 'v[0-9]*' --sort=-v:refname | head -1)
  if [[ -z "$latest" ]]; then
    base="0.0.1"
  else
    IFS=. read -r major minor patch <<<"${latest#v}"
    base="$major.$minor.$((patch + 1))"
  fi
  version="${base}-dev.$(date -u +%Y%m%d)+${COMMIT}"
  channel="edge"
fi

case "$MODE" in
  --channel) echo "$channel" ;;
  --commit) echo "$COMMIT" ;;
  version) echo "$version" ;;
  *) echo "usage: scripts/version.sh [--channel|--commit]" >&2; exit 2 ;;
esac
