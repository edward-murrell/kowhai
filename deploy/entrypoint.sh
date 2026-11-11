#!/bin/sh
# Runs kowhai, under Litestream when KOWHAI_LITESTREAM=1. Litestream
# restores the database from the replica on first start, then replicates
# every change to the backup bucket while kowhai runs.
set -eu

if [ "${1:-serve}" = "serve" ] && [ "${KOWHAI_LITESTREAM:-0}" = "1" ]; then
  litestream restore -config /etc/litestream.yml -if-db-not-exists -if-replica-exists "$KOWHAI_DB_PATH"
  exec litestream replicate -config /etc/litestream.yml -exec "kowhai serve"
fi
exec kowhai "$@"
