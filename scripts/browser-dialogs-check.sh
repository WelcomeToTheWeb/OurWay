#!/usr/bin/env bash
#
# 2.2-9: "Confirmation is a state of the control, not a browser dialog …
# `window.confirm()` is banned" (DESIGN.md §6). A rule that is only written
# down is a rule that drifts back: the ConfirmSheet pattern existed and ten
# destructive actions kept using the native dialog (`buglist.md` M19), because
# nothing failed when someone typed `confirm(`.
#
# Browser dialogs are unthemeable, unlocalisable (the copy is a string built in
# English, so a German operator is asked to destroy data in English), and
# automation dismisses them — which is why the e2e suite used to accept a dialog
# to end a session at all.
#
# This check fails on any native dialog call in shipped console source.
# Destructive confirmation goes through ConfirmSheet (web/src/components/),
# with its copy in en/de/fr under `confirm.<action>.*`.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# `confirm(` / `alert(` / `prompt(` as a call, with or without the `window.`
# prefix. The lookbehind keeps our own names out of the net: confirmRequest,
# pendingConfirm, onConfirm, ConfirmSheet never match. Comment lines are
# skipped — the ban is quoted in prose (here and in ConfirmSheet.tsx), and a
# comment that names the rule is not a call of it.
hits=$(grep -rnP --include='*.tsx' --include='*.ts' \
  '(?<![\w$.])(window\.)?\b(confirm|alert|prompt)\(' web/src \
  | grep -vP ':\d+:(\s*)(\*|//|/\*)' || true)

if [ -n "$hits" ]; then
  echo "browser dialogs are banned (DESIGN.md §6) — the console found:" >&2
  printf '%s\n' "$hits" >&2
  echo >&2
  echo "Confirm in the page instead: render <ConfirmSheet request={…} onConfirm={…} onCancel={…} />" >&2
  echo "and put the copy in all three locales under confirm.<action>.* (task 2.2-9)." >&2
  exit 1
fi

count=$(find web/src -name '*.tsx' -o -name '*.ts' | wc -l)
echo "browser-dialogs check: 0 native dialogs in ${count} console sources"
