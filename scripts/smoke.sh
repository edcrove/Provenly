#!/usr/bin/env bash
# Smoke test of a running docker environment through its web entry point
# (nginx -> api -> postgres): scripts/smoke.sh http://localhost:3000
set -euo pipefail
web="${1:-http://localhost:3000}"
curl_() { curl -fsS --noproxy '*' "$@"; }
fail() { echo "smoke: $*" >&2; exit 1; }

curl_ "$web/healthz" | grep -q '"ok"' || fail "health check through the proxy"
curl_ -o /dev/null "$web/test-runs/1" || fail "SPA route not served"
tc=$(curl_ -H 'Content-Type: application/json' -X POST "$web/api/v1/test-cases" \
  -d '{"title":"Smoke test case","automated":true}' | sed -n 's/^{"id":\([0-9]*\).*/\1/p')
[ -n "$tc" ] || fail "could not create a test case"
run="smoke$(date +%s)"
curl_ -o /dev/null -H 'Content-Type: application/xml' -X POST \
  --data-binary "<testsuite name=\"smoke\"><testcase name=\"smoke TC-$tc\"/></testsuite>" \
  "$web/api/v1/ingestion/junit?provider=github&runId=$run&runAttempt=1" || fail "ingestion"
curl_ "$web/api/v1/test-runs?pageSize=100" | grep -q "github:$run:1" || fail "ingested run not listed"
echo "smoke: ok ($web, TC-$tc, github:$run:1)"
