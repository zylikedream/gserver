#!/bin/bash
# Clean all data from Redis and PostgreSQL.
# Usage:
#   PGPASSWORD='@zyc0131' bash hack/clean-data.sh          # 默认 local
#   PGPASSWORD='@zyc0131' bash hack/clean-data.sh local    # local
#   PGPASSWORD='@zyc0131' bash hack/clean-data.sh k8s      # k8s

set -e

ENV="${1:-local}"
case "$ENV" in
  local)
    REDIS_DB=1
    PG_DB="gserver"
    ;;
  k8s)
    REDIS_DB=0
    PG_DB="postgres"
    ;;
  *)
    echo "Usage: $0 [local|k8s]" >&2
    exit 1
    ;;
esac



echo "=== Cleaning Redis ==="
redis-cli -n "$REDIS_DB" FLUSHDB
echo "Redis done."

echo ""
echo "=== Cleaning PostgreSQL ==="
psql -h 127.0.0.1 -p 5432 -U postgres -d "$PG_DB" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
echo "PostgreSQL done."

echo ""
echo "All clean."
