#!/bin/sh
# Healthy when the last successful backup is at most BACKUP_MAX_AGE_HOURS old (default 26: a daily schedule plus slack).
set -eu
marker="${BACKUP_DIR:-/backups}/.last-success"
test -n "$(find "$marker" -mmin -"$(( ${BACKUP_MAX_AGE_HOURS:-26} * 60 ))" 2>/dev/null)"
