#!/usr/bin/env bash

set -o nounset
set -euo pipefail
set -o errexit

su - postgres -c "mkdir -p /tmp/postgres/data"
su - postgres -c "mkdir -p /tmp/postgres/sockets"

su - postgres -c "/usr/pgsql-12/bin/initdb -D /tmp/postgres/data"
su - postgres -c "/usr/pgsql-12/bin/pg_ctl -D /tmp/postgres/data/ -l /tmp/postgres/logfile start"

echo "Create superuser role admin"
su - postgres -c "createuser -s admin -h localhost"

echo "Create admin database"
su - postgres -c "createdb admin -h localhost"
