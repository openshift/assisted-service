#!/usr/bin/env bash

set -o nounset
set -euo pipefail
set -o errexit

su - postgres -c "mkdir -p /tmp/postgres/data"

su - postgres -c "/usr/bin/initdb -D /tmp/postgres/data"
su - postgres -c "/usr/bin/pg_ctl -D /tmp/postgres/data/ -l /tmp/postgres/logfile start"
