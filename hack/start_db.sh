#!/usr/bin/env bash

set -o nounset
set -euo pipefail
set -o errexit

mkdir -p /tmp/postgres/data
mkdir -p /tmp/postgres/sockets

initdb -D /tmp/postgres/data -U postgres
pg_ctl -D /tmp/postgres/data -l /tmp/postgres/logfile -o'-k /tmp/postgres/sockets -p 5433' start
