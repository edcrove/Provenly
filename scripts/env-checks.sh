#!/usr/bin/env bash
# Checks of the docker environments that the CI smoke steps do not cover (Dockerized Environments card):
# image facts, data isolation between environments, a snapshot used as a seed, qa dump/restore, prod's
# guards (refusals without CONFIRM=prod, a dump before a confirmed reset), the CA build-cache key and hot
# reload. Run after `docker compose up` (demo) and `make up ENV=qa` in the CI docker job.
# It creates envs/prod.env, writes backups/ and resets prod: CI only.
set -euo pipefail
[ "${CI:-}" = true ] || { echo "env-checks: CI only (it resets prod and writes backups/)" >&2; exit 1; }
cd "$(dirname "$0")/.."
# Failures are also GitHub annotations, readable from the check run without the raw log.
fail() { echo "env-checks: $*" >&2; [ -z "${GITHUB_ACTIONS:-}" ] || echo "::error title=env-checks::$*"; exit 1; }
trap 'rc=$?; [ -z "${GITHUB_ACTIONS:-}" ] || echo "::error title=env-checks::line $LINENO: $BASH_COMMAND (exit $rc)"' ERR
ok() { echo "env-checks: ok - $*"; }
curl_() { curl -fsS --noproxy '*' "$@"; }
# shellcheck source=lib/session.sh
source scripts/lib/session.sh
# api BASE curl-args...: an authenticated call (each environment has its own users and session secret).
api() { local jar; jar=$(provenly_login "$1"); shift; curl_ -b "$jar" "$@"; rm -f "$jar"; }
run="$(date +%s)"  # unique titles: a re-run never counts an earlier run's rows
count() { api "$1" "$1/api/v1/test-cases?pageSize=100" | grep -o "\"title\":\"$2\"" | wc -l; }

# Images: API on alpine as a non-root user, both with healthchecks; the web image is nginx.
[ "$(docker image inspect provenly-api:local --format '{{.Config.User}}')" = provenly ] || fail "api image must run as provenly"
docker run --rm --entrypoint cat provenly-api:local /etc/os-release | grep -q '^ID=alpine' || fail "api image is not alpine"
docker image inspect provenly-api:local --format '{{json .Config.Healthcheck.Test}}' | grep -q readyz || fail "api healthcheck"
docker image inspect provenly-web:local --format '{{json .Config.Healthcheck.Test}}' | grep -q wget || fail "web healthcheck"
docker run --rm --entrypoint nginx provenly-web:local -v 2>&1 | grep -q nginx || fail "web image is not nginx"
ok "images"

# Isolation: data written to qa never shows up in demo.
api http://localhost:8180 -o /dev/null -H 'Content-Type: application/json' -X POST http://localhost:8180/api/v1/test-cases \
  -d "{\"title\":\"env-checks qa only $run\",\"automated\":false}"
[ "$(count http://localhost:8180 "env-checks qa only $run")" = 1 ] || fail "qa did not keep its test case"
[ "$(count http://localhost:8080 "env-checks qa only $run")" = 0 ] || fail "demo sees qa data"
ok "isolation"

# A snapshot of qa becomes a seed: an empty environment started with it holds qa's data; demo-reset undoes it.
make --no-print-directory seed-snapshot FROM=qa NAME=envcheck >/dev/null
rm_seed() { rm -f seeds/envcheck.sql; }
SEED=envcheck docker compose --env-file envs/demo.env down -v >/dev/null 2>&1
SEED=envcheck docker compose --env-file envs/demo.env up -d --wait >/dev/null 2>&1
[ "$(count http://localhost:8080 "env-checks qa only $run")" = 1 ] || fail "seed from a qa snapshot"
make --no-print-directory demo-reset >/dev/null 2>&1
[ "$(count http://localhost:8080 "env-checks qa only $run")" = 0 ] || fail "demo-reset did not restore the demo seed"
rm_seed
ok "snapshot as seed, demo-reset"

# qa dump/restore round trip: a test case created after the dump is gone after restoring it.
make --no-print-directory db-dump ENV=qa >/dev/null
dump=$(ls -t backups/qa-*.sql | head -1)
api http://localhost:8180 -o /dev/null -H 'Content-Type: application/json' -X POST http://localhost:8180/api/v1/test-cases \
  -d "{\"title\":\"env-checks after dump $run\",\"automated\":false}"
make --no-print-directory db-restore ENV=qa FILE="$dump" >/dev/null 2>&1
[ "$(count http://localhost:8180 "env-checks after dump $run")" = 0 ] || fail "db-restore did not bring qa back to the dump"
[ "$(count http://localhost:8180 "env-checks qa only $run")" = 1 ] || fail "db-restore lost data from the dump"
ok "db-dump / db-restore"

# prod: credentials outside the repo, every destructive target refused without CONFIRM=prod.
{ git check-ignore -q envs/prod.env && git check-ignore -q backups/; } || fail "envs/prod.env and backups/ must be gitignored"
[ -f envs/prod.env ] || cp envs/prod.env.example envs/prod.env
# prod needs its own session secret and first administrator (CI values; a real prod.env sets its own).
if ! grep -q '^PROVENLY_JWT_SECRET=.\{32,\}' envs/prod.env; then
  sed -i '/^PROVENLY_\(JWT_SECRET\|ADMIN_USERNAME\|ADMIN_PASSWORD\)=$/d' envs/prod.env
  printf 'PROVENLY_JWT_SECRET=%s\nPROVENLY_ADMIN_USERNAME=admin\nPROVENLY_ADMIN_PASSWORD=%s\n' "$(openssl rand -hex 32)" "$(openssl rand -hex 12)" >> envs/prod.env
fi
prod_api() { PROVENLY_ADMIN_PASSWORD="$(sed -n 's/^PROVENLY_ADMIN_PASSWORD=//p' envs/prod.env | tail -1)" api "$@"; }
for target in "db-reset ENV=prod" "db-restore ENV=prod FILE=$dump" "seed-snapshot FROM=prod NAME=x"; do
  # shellcheck disable=SC2086
  out=$(make --no-print-directory $target 2>&1) && fail "make $target ran without CONFIRM=prod"
  echo "$out" | grep -q "CONFIRM=prod" || fail "make $target: $out"
done
ok "prod refuses without CONFIRM=prod"

# prod starts empty on its own ports; a confirmed reset dumps it first.
make --no-print-directory up ENV=prod >/dev/null 2>&1
prod_api http://localhost:8280 http://localhost:8280/api/v1/test-runs | grep -q '"totalItems":0' || fail "prod must start empty"
prod_api http://localhost:8280 -o /dev/null -H 'Content-Type: application/json' -X POST http://localhost:8280/api/v1/test-cases \
  -d "{\"title\":\"env-checks prod data $run\",\"automated\":false}"
before=$( (ls backups/prod-*.sql 2>/dev/null || true) | wc -l)
make --no-print-directory db-reset ENV=prod CONFIRM=prod >/dev/null 2>&1
[ "$(ls backups/prod-*.sql | wc -l)" -gt "$before" ] || fail "db-reset ENV=prod did not dump first"
grep -q "env-checks prod data $run" "$(ls -t backups/prod-*.sql | head -1)" || fail "the prod dump misses its data"
[ "$(prod_api http://localhost:8280 "http://localhost:8280/api/v1/test-cases?pageSize=100" | grep -o "env-checks prod data $run" | wc -l)" = 0 ] || fail "db-reset ENV=prod kept the data"
make --no-print-directory down ENV=prod >/dev/null 2>&1
ok "prod: empty start, dump before a confirmed reset"

# A new CA re-runs the layer that installs it (build secrets are not part of the cache key).
ca="${ENV_CHECKS_CA:-/etc/ssl/certs/ca-certificates.crt}"
EXTRA_CA_CERT="$ca" make --no-print-directory -s up ENV=qa >/tmp/ca-build.log 2>&1 || true
hash=$(sha256sum "$ca" | cut -d' ' -f1)
grep -q "extra CA: $hash" /tmp/ca-build.log || fail "adding a CA reused the cached layer built without it"
ok "CA build-cache key"

# Hot reload: air rebuilds the API on a source change; the UI is the Vite dev server.
make --no-print-directory dev ENV=qa >/dev/null 2>&1
api_logs() { docker compose --env-file envs/qa.env -f docker-compose.yml -f docker-compose.dev.yml logs --since "$since" api 2>&1; }
since=$(date +%s)
touch backend/cmd/provenly/main.go
for _ in $(seq 1 60); do
  api_logs | grep -q 'building' && break
  sleep 1
done
api_logs | grep -q 'building' || fail "air did not rebuild the API"
for _ in $(seq 1 60); do curl_ -o /dev/null http://localhost:8180/readyz 2>/dev/null && break; sleep 1; done
curl_ http://localhost:3100/ | grep -q '@vite/client' || fail "the dev UI is not the Vite dev server"
api http://localhost:3100 -o /dev/null http://localhost:3100/api/v1/test-runs || fail "the Vite dev server does not proxy /api"
make --no-print-directory up ENV=qa >/dev/null 2>&1
ok "hot reload"
