#!/usr/bin/env bash
# Repository consistency checks that no compiler catches (run by `make lint` and CI):
# - every screenshot is listed in docs/screenshots/README.md and every listed one exists;
# - no doc tells users to install the reporter from the npm registry while the @provenly scope is not ours;
# - workflows grant no write permission by default and never leave the checkout token on disk.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
fail=0
err() { echo "docs-check: $*" >&2; fail=1; }

index=docs/screenshots/README.md
for f in docs/screenshots/*.png; do
  grep -q "\`$(basename "$f")\`" "$index" || err "$f is not listed in $index"
done
for f in $(grep -o '`[0-9][0-9]-[a-z0-9-]*\.png`' "$index" | tr -d '`'); do
  [ -f "docs/screenshots/$f" ] || err "$index lists $f, which does not exist"
done

if grep -rnE 'npm (i|install)( -D| --save-dev)? @provenly/' README.md docs reporters/playwright/README.md; then
  err "a doc installs @provenly/* from npm; the scope is not registered to this project (install from a checkout)"
fi

for wf in .github/workflows/*.yml; do
  if awk '/^permissions:/{p=1; next} p && /^[^ ]/{p=0} p && /: *write/' "$wf" | grep -q .; then
    err "$wf grants a write permission at the top level (grant it per job)"
  fi
  checkouts=$(grep -c 'uses: actions/checkout@' "$wf" || true)
  safe=$(grep -A2 'uses: actions/checkout@' "$wf" | grep -c 'persist-credentials: false' || true)
  [ "$checkouts" = "$safe" ] || err "$wf: $((checkouts - safe)) checkout(s) without persist-credentials: false"
done

[ "$fail" = 0 ] && echo "docs-check: OK"
exit "$fail"
