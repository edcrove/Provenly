# Sourced by the automated-suite scripts. Unless E2E_DATABASE_URL points at an
# existing database (CI provides a service container), starts the in-memory
# Postgres of docker-compose.e2e.yml and destroys it when the script exits.
ephemeral_db() {
  local root="$1"
  if [ -n "${E2E_DATABASE_URL:-}" ]; then
    return
  fi
  local compose=(docker compose -f "$root/docker-compose.e2e.yml")
  echo "==> starting an ephemeral database (destroyed on exit)"
  "${compose[@]}" up -d --wait >/dev/null
  # shellcheck disable=SC2064
  trap "${compose[*]} down -v >/dev/null 2>&1" EXIT
  export E2E_DATABASE_URL="postgres://provenly:provenly@localhost:${E2E_DB_PORT:-5439}/provenly_e2e?sslmode=disable"
}
