#!/bin/sh
# One backup of the environment's database (card #63), run by the compose `backup` service (scripts/backup/entrypoint.sh):
# pg_dump in custom format (-Fc, restore with `make db-restore FILE=…`) to a temporary file renamed when complete, a
# last-success marker for the healthcheck, then rotation of dumps older than BACKUP_RETENTION_DAYS. Rotation runs only
# after a successful dump: a failing database never costs the dumps already taken. The dump holds no
# PROVENLY_SECRETS_KEY: keep that key elsewhere, or the sealed webhook and connector secrets cannot be read back.
set -eu
dir="${BACKUP_DIR:-/backups}"
keep="${BACKUP_RETENTION_DAYS:-14}"
case "$keep" in '' | *[!0-9]*) echo "backup: BACKUP_RETENTION_DAYS must be a whole number of days, got '$keep'" >&2; exit 2 ;; esac
name="$dir/${PROVENLY_ENV:-provenly}-$(date -u +%Y%m%d-%H%M%S).dump"
tmp="$name.partial"
trap 'rm -f "$tmp"' EXIT
mkdir -p "$dir"
pg_dump -Fc --no-owner --no-privileges -f "$tmp"
mv "$tmp" "$name"
date -u +%Y-%m-%dT%H:%M:%SZ > "$dir/.last-success"
echo "backup: saved $name"
# Rotation: dumps (and partial files a killed run left) older than the retention.
find "$dir" -maxdepth 1 \( -name '*.dump' -o -name '*.dump.partial' \) -mtime +"$keep" -print -exec rm -f {} \; |
  sed 's/^/backup: removed /'
