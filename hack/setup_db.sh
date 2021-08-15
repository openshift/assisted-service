#!/usr/bin/env bash

mkdir -p /tmp/postgres/data
/usr/pgsql-13/bin/initdb -D /tmp/postgres/data/
/usr/pgsql-13/bin/pg_ctl -D /tmp/postgres/data/ -l /tmp/postgres/logfile start

echo "Connect as postgres user and create superuser role admin"
createuser -U postgres -s admin

echo "Create admin database"
createdb admin
