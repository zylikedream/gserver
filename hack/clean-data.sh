#!/bin/bash
# Clean all data from Redis and PostgreSQL for local development.
# Usage: PGPASSWORD='@zyc0131' bash hack/clean-data.sh

set -e

REDIS_DB=0
PG_DB="gserver"

echo "=== Cleaning Redis ==="
redis-cli -n "$REDIS_DB" FLUSHDB
echo "Redis done."

echo ""
echo "=== Cleaning PostgreSQL ==="
psql -h 127.0.0.1 -p 5432 -U postgres -d "$PG_DB" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
echo "PostgreSQL done."

echo ""
echo "All clean."
