#!/usr/bin/env bash

set -euo pipefail

PG_USER=postgres
PG_DATABASE=postgres
PG_HOST=127.0.0.1
PG_PORT=5432
export PGPASSWORD=admin

if [ -x "$(command -v pg_isready)" ]; then
    until pg_isready -U ${PG_USER} --dbname ${PG_DATABASE} --host ${PG_HOST} --port ${PG_PORT}; do sleep 1; done
else
    until psql -h ${PG_HOST} -U ${PG_USER} -d ${PG_DATABASE} -c "select 1" >/dev/null 2>&1; do sleep 1; done
fi
