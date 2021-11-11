#!/usr/bin/env bash

set -o nounset
set -euo pipefail
set -o errexit

mkdir -p /tmp/postgres/data
mkdir -p /tmp/postgres/sockets

/usr/bin/initdb -D /tmp/postgres/data -U postgres
/opt/rh/rh-postgresql12/root/usr/bin/pg_ctl -D /tmp/postgres/data -l /tmp/postgres/logfile -o'-k /tmp/postgres/sockets' start
