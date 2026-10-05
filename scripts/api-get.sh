#!/usr/bin/env bash
# GET an API path signed in as the demo administrator (or PROVENLY_ADMIN_*):
#   scripts/api-get.sh http://localhost:8080 /api/v1/test-runs
set -euo pipefail
# shellcheck source=lib/session.sh
source "$(dirname "$0")/lib/session.sh"
jar=$(provenly_login "$1")
trap 'rm -f "$jar"' EXIT
curl -fsS --noproxy '*' -b "$jar" "$1$2"
