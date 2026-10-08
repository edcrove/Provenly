#!/usr/bin/env bash
# Smoke test of a running docker environment through its web entry point
# (nginx -> api -> postgres): scripts/smoke.sh http://localhost:3000
set -euo pipefail
web="${1:-http://localhost:3000}"
curl_() { curl -fsS --noproxy '*' "$@"; }
fail() { echo "smoke: $*" >&2; exit 1; }
# shellcheck source=lib/session.sh
source "$(dirname "$0")/lib/session.sh"

curl_ "$web/healthz" | grep -q '"ok"' || fail "health check through the proxy"
curl_ "$web/readyz" | grep -q '"ok"' || fail "readiness (database) through the proxy"
curl_ -o /dev/null "$web/test-runs/1" || fail "SPA route not served"
[ "$(curl -s --noproxy '*' -o /dev/null -w '%{http_code}' "$web/api/v1/test-cases")" = 401 ] || fail "the API answers without a session"
jar=$(provenly_login "$web") || fail "sign-in through the proxy"
# Security headers on the app and the API; the session cookie is Secure only when the browser used https (a TLS
# proxy in front says so in X-Forwarded-Proto, which nginx passes through).
for path in / /api/v1/auth/me; do
  h=$(curl -s --noproxy '*' -o /dev/null -D - "$web$path")
  for want in "content-security-policy: default-src 'self'" "x-frame-options: DENY" "x-content-type-options: nosniff" "referrer-policy: no-referrer"; do
    grep -qi "^$want" <<<"$h" || fail "$path lacks the header: $want"
  done
done
# The API sends its own copies (for deployments without this proxy); through the proxy each header comes once.
[ "$(curl -s --noproxy '*' -o /dev/null -D - "$web/api/v1/auth/me" | grep -ci '^x-content-type-options:')" = 1 ] ||
  fail "the API's security headers are repeated through the proxy"
login_cookie() { curl -s --noproxy '*' -o /dev/null -D - -H 'Content-Type: application/json' "$@" -X POST "$web/api/v1/auth/login" \
  -d "{\"username\":\"${PROVENLY_ADMIN_USERNAME:-admin}\",\"password\":\"${PROVENLY_ADMIN_PASSWORD:-provenly-demo}\"}" | grep -i '^set-cookie: provenly_session='; }
login_cookie -H 'X-Forwarded-Proto: https' | grep -qi '; Secure' || fail "the session cookie is not Secure behind a TLS proxy"
if login_cookie | grep -qi '; Secure'; then fail "the session cookie is Secure on plain http"; fi
tc=$(curl_ -b "$jar" -H 'Content-Type: application/json' -X POST "$web/api/v1/test-cases" \
  -d '{"title":"Smoke test case","automated":true}' | sed -n 's/^{"id":\([0-9]*\).*/\1/p')
[ -n "$tc" ] || fail "could not create a test case"
key=$(curl_ -b "$jar" -H 'Content-Type: application/json' -X POST "$web/api/v1/projects/TC/api-keys" \
  -d '{"name":"smoke"}' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
[ -n "$key" ] || fail "could not create an API key"
run="smoke$(date +%s)"
[ "$(curl -s --noproxy '*' -o /dev/null -w '%{http_code}' -H 'Content-Type: application/xml' -X POST --data-binary '<testsuite/>' \
  "$web/api/v1/ingestion/junit?provider=github&runId=$run&runAttempt=1")" = 401 ] || fail "ingestion answers without a key"
curl_ -o /dev/null -H "Authorization: Bearer $key" -H 'Content-Type: application/xml' -X POST \
  --data-binary "<testsuite name=\"smoke\"><testcase name=\"smoke TC-$tc\"/></testsuite>" \
  "$web/api/v1/ingestion/junit?provider=github&runId=$run&runAttempt=1" || fail "ingestion with an API key through the proxy"
curl_ -b "$jar" "$web/api/v1/test-runs?pageSize=100" | grep -q "github:$run:1" || fail "ingested run not listed"
echo "smoke: ok ($web, TC-$tc, github:$run:1)"
