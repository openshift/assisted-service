#!/usr/bin/env bash

set -o nounset
set -euo pipefail
set -o errexit

mkdir -p /tmp/postgres/data
mkdir -p /tmp/postgres/sockets

initdb -D /tmp/postgres/data -U postgres

# We store the data files in the `/tmp` directory assuming that it will be a `tmpfs` and therefore
# very fast. But in some environments it may not be, as it may be mapped to a real persistent file
# system. Disabling `fsync` speeds things up in those environment, at the cost of risking data loss,
# but we don't really care about that, otherwise we woudn't be using `/tmp`.
echo "fsync=off" >> /tmp/postgres/data/postgresql.conf

pg_ctl -D /tmp/postgres/data -l /tmp/postgres/logfile -o'-k /tmp/postgres/sockets -p 5433' start
