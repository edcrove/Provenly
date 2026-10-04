#!/usr/bin/env bash
# Runs the Playwright E2E journeys against the coverage-instrumented stack and
# leaves raw coverage evidence in e2e/coverage/{backend,frontend}.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=lib/ephemeral-db.sh
source "$root/scripts/lib/ephemeral-db.sh"
ephemeral_db "$root"
export PROVENLY_DATABASE_URL="$E2E_DATABASE_URL"

echo "==> building coverage-instrumented backend (go build -cover)"
(cd "$root/backend" && go build -cover -covermode=atomic -coverpkg=./... -o bin/provenly-cover ./cmd/provenly)

echo "==> building istanbul-instrumented frontend (vite-plugin-istanbul)"
(cd "$root/frontend" && VITE_COVERAGE=true npx vite build --logLevel error)

rm -rf "$root/e2e/coverage"
mkdir -p "$root/e2e/coverage/backend" "$root/e2e/coverage/frontend"

echo "==> resetting the E2E database"
for cmd in up reset; do
  if ! GOCOVERDIR="$root/e2e/coverage/backend" "$root/backend/bin/provenly-cover" migrate "$cmd" >/dev/null 2>&1; then
    echo "error: cannot reach the E2E database ($PROVENLY_DATABASE_URL)." >&2
    exit 1
  fi
done

echo "==> running Playwright journeys"
(cd "$root/e2e" && npx playwright test "$@")
