#!/usr/bin/env bash
# Captures screenshots of every relevant UI flow into docs/screenshots.
# Runs on an ephemeral database (see scripts/lib/ephemeral-db.sh).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=lib/ephemeral-db.sh
source "$root/scripts/lib/ephemeral-db.sh"
ephemeral_db "$root"
export PROVENLY_DATABASE_URL="$E2E_DATABASE_URL"

(cd "$root/backend" && go build -o bin/provenly ./cmd/provenly)
(cd "$root/frontend" && npx vite build --logLevel error)
for cmd in up reset; do "$root/backend/bin/provenly" migrate "$cmd" >/dev/null 2>&1; done
rm -f "$root"/docs/screenshots/*.png
mkdir -p "$root/docs/screenshots"
(cd "$root/e2e" && npx playwright test --config playwright.screenshots.config.ts "$@")
