#!/usr/bin/env bash

set -o nounset
set -euo pipefail
set -o errexit

/usr/bin/pg_ctl -D /tmp/postgres/data/ -l /tmp/postgres/logfile start 

createuser -s admin -h localhost
createdb installer -h localhost

# To keep the db container up and running
sleep infinity
