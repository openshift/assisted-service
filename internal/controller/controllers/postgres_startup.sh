#!/bin/bash
# postgres_startup.sh - Wrapper script for PostgreSQL container startup
#
# This script checks if a PostgreSQL major version upgrade is needed before starting
# the database. It compares the data directory version (PG_VERSION) with the container's
# PostgreSQL version and enables pg_upgrade only when necessary.
#
# See docs/dev/postgresql-upgrade.md for details.

set -e

PGDATA=/var/lib/pgsql/data/userdata

echo "=== PostgreSQL Startup Check ==="

if [ -f "$PGDATA/PG_VERSION" ]; then
    DATA_VERSION=$(cat "$PGDATA/PG_VERSION")
    echo "Data directory version: $DATA_VERSION"
    echo "Container image version: $POSTGRESQL_VERSION"

    if [ "$DATA_VERSION" != "$POSTGRESQL_VERSION" ]; then
        echo "Version mismatch detected - enabling pg_upgrade (hardlink mode)"
        export POSTGRESQL_UPGRADE=hardlink
    else
        echo "Versions match - normal startup"
    fi
else
    echo "No existing data directory - fresh initialization"
fi

exec run-postgresql
