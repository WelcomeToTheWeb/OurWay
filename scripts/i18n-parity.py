#!/usr/bin/env python3
"""i18n key parity: en, de and fr must define exactly the same key paths.

2.2's i18n lockstep finish needs a machine-checked floor, because the parity
breaks silently: a key added to en.json renders in English for everyone, and a
key missing from de.json renders the raw key path in the console for a German
operator. This script fails the build on any asymmetry, and prints the offending
paths so the fix is obvious.

Usage: scripts/i18n-parity.py [path/to/locales ...]
"""
import json
import sys
from pathlib import Path

LOCALES = ("en", "de", "fr")


def flatten(obj, prefix=""):
    keys = set()
    if isinstance(obj, dict):
        for k, v in obj.items():
            path = f"{prefix}.{k}" if prefix else k
            if isinstance(v, dict):
                keys |= flatten(v, path)
            else:
                keys.add(path)
    return keys


def main() -> int:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else "web/src/i18n/locales")
    trees = {}
    for code in LOCALES:
        path = root / f"{code}.json"
        if not path.exists():
            print(f"i18n-parity: missing locale file {path}", file=sys.stderr)
            return 1
        trees[code] = flatten(json.loads(path.read_text()))

    all_keys = set().union(*trees.values())
    ok = True
    for code in LOCALES:
        missing = sorted(all_keys - trees[code])
        if missing:
            ok = False
            print(f"i18n-parity: {code}.json is missing {len(missing)} key path(s):")
            for key in missing:
                print(f"  {code}: {key}")

    if not ok:
        print(
            "i18n-parity: locales must stay in lockstep (CONTRIBUTING.md). Add the "
            "translated string in every locale file, in the same nesting.",
            file=sys.stderr,
        )
        return 1

    print(f"i18n-parity: en/de/fr agree on {len(all_keys)} key paths")
    return 0


if __name__ == "__main__":
    sys.exit(main())
