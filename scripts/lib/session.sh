# shellcheck shell=bash
# Signs in to a Provenly API (prototype feature 2: every API route but health,
# sign-in and ingestion needs a session) and prints a curl cookie-jar path.
# Credentials: PROVENLY_ADMIN_USERNAME / PROVENLY_ADMIN_PASSWORD, by default the
# local demo administrator of envs/demo.env and envs/qa.env.
provenly_login() { # base-url (API or web entry point) -> cookie jar path
  local jar
  jar="$(mktemp)"
  curl -fsS --noproxy '*' -c "$jar" -o /dev/null -H 'Content-Type: application/json' -X POST "$1/api/v1/auth/login" \
    -d "{\"username\":\"${PROVENLY_ADMIN_USERNAME:-admin}\",\"password\":\"${PROVENLY_ADMIN_PASSWORD:-provenly-demo}\"}" ||
    { echo "cannot sign in at $1 as ${PROVENLY_ADMIN_USERNAME:-admin}" >&2; return 1; }
  echo "$jar"
}
