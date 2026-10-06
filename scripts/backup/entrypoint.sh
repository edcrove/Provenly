#!/bin/sh
# The compose `backup` service (profile `backup`): a first backup now, so a wrong setting shows at start, then one on
# BACKUP_SCHEDULE (cron syntax, default daily at 03:17 UTC) with busybox crond. The healthcheck reads the marker the
# last successful backup wrote.
set -eu
schedule="${BACKUP_SCHEDULE:-17 3 * * *}"
/opt/backup/backup.sh
echo "$schedule /opt/backup/backup.sh >> /proc/1/fd/1 2>> /proc/1/fd/2" > /etc/crontabs/root
echo "backup: next backups on '$schedule' (UTC), keeping ${BACKUP_RETENTION_DAYS:-14} days in ${BACKUP_DIR:-/backups}"
exec crond -f -l 8
