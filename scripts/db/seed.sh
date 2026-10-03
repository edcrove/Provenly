#!/bin/sh
# Runs once per start in the `seed` service. When SEED names a snapshot
# (seeds/<SEED>.sql) and the database is still EMPTY (never migrated), the
# snapshot is restored; `migrate` then applies any newer migrations on top.
# A database that already has data is never touched.
set -eu

if [ -z "${SEED:-}" ]; then
  echo "seed: no snapshot configured for this environment (SEED is empty)"
  exit 0
fi
if [ "$(psql -tAc "SELECT to_regclass('public.goose_db_version') IS NOT NULL")" = "t" ]; then
  echo "seed: database already initialized; keeping its data"
  exit 0
fi
file="/seeds/${SEED}.sql"
if [ ! -f "$file" ]; then
  echo "seed: snapshot $file not found" >&2
  exit 1
fi
psql -q -v ON_ERROR_STOP=1 -f "$file" >/dev/null
echo "seed: restored snapshot '${SEED}'"
