#!/bin/bash
# postgres_hop.sh - One-shot PostgreSQL major-version hop for init containers.
#
# sclorg images can only upgrade from POSTGRESQL_PREV_VERSION. ACM/MCE EUS
# skip-upgrades (for example ACM 2.15 PG12 → ACM 2.17 PG15) therefore need
# intermediate images. This script runs in an init container: it upgrades
# exactly one hop if needed, then exits so the next hop or the main postgres
# container can run.
#
# See docs/dev/postgresql-upgrade.md for details.

set -eu

PGDATA="${PGDATA:-/var/lib/pgsql/data/userdata}"
CONTAINER_SCRIPTS_PATH="${CONTAINER_SCRIPTS_PATH:-/usr/share/container-scripts/postgresql}"
APP_DATA="${APP_DATA:-/opt/app-root}"

echo "=== PostgreSQL hop (${POSTGRESQL_VERSION:-unknown}, prev=${POSTGRESQL_PREV_VERSION:-unknown}) ==="

if [ ! -f "$PGDATA/PG_VERSION" ]; then
    echo "No existing data directory - skipping hop"
    exit 0
fi

DATA_VERSION="$(tr -d '[:space:]' < "$PGDATA/PG_VERSION")"
echo "Data directory version: $DATA_VERSION"
echo "Hop image version: ${POSTGRESQL_VERSION:-}"

if [ -z "${POSTGRESQL_VERSION:-}" ]; then
    echo "ERROR: POSTGRESQL_VERSION is not set in this image" >&2
    exit 1
fi

# Skip if data is already at or past this hop. Only read PG_VERSION — do not
# start postgres, because a newer data directory cannot be opened by an older hop image.
if [ "$DATA_VERSION" -ge "$POSTGRESQL_VERSION" ] 2>/dev/null; then
    echo "Data is already at version $DATA_VERSION - skipping this hop"
    exit 0
fi

if [ -z "${POSTGRESQL_PREV_VERSION:-}" ]; then
    echo "ERROR: POSTGRESQL_PREV_VERSION is not set in this image" >&2
    exit 1
fi

if [ "$DATA_VERSION" != "$POSTGRESQL_PREV_VERSION" ]; then
    echo "ERROR: this hop image can only upgrade from ${POSTGRESQL_PREV_VERSION}, not ${DATA_VERSION}." >&2
    echo "An earlier hop init container is missing from the upgrade path." >&2
    exit 1
fi

echo "Upgrading PostgreSQL data from ${DATA_VERSION} to ${POSTGRESQL_VERSION} (hardlink mode)"
export POSTGRESQL_UPGRADE=hardlink
export ENABLE_REPLICATION="${ENABLE_REPLICATION:-false}"

export_vars="$(cgroup-limits)"
# shellcheck disable=SC2086
export $export_vars

# shellcheck disable=SC1091
source "${CONTAINER_SCRIPTS_PATH}/common.sh"

set_pgdata
process_extending_files \
    "${APP_DATA}/src/postgresql-pre-start" \
    "${CONTAINER_SCRIPTS_PATH}/pre-start"
check_env_vars
generate_passwd_file
generate_postgresql_config

try_pgupgrade

echo "Hop to PostgreSQL ${POSTGRESQL_VERSION} complete"
