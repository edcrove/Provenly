#!/usr/bin/env bash
# Captures screenshots of every relevant UI flow into docs/screenshots.
# Resets the E2E database (make up first).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
export PROVENLY_DATABASE_URL="${E2E_DATABASE_URL:-postgres://provenly:provenly@localhost:5432/provenly_e2e?sslmode=disable}"

(cd "$root/backend" && go build -o bin/provenly ./cmd/provenly)
(cd "$root/frontend" && npx vite build --logLevel error)
for cmd in up reset; do "$root/backend/bin/provenly" migrate "$cmd" >/dev/null 2>&1; done
rm -f "$root"/docs/screenshots/*.png
mkdir -p "$root/docs/screenshots"
(cd "$root/e2e" && npx playwright test --config playwright.screenshots.config.ts "$@")
