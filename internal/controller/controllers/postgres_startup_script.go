package controllers

import _ "embed"

// PostgresStartupScript is a wrapper script that conditionally enables pg_upgrade
// only when a PostgreSQL major version upgrade is detected. This avoids the issue
// where setting POSTGRESQL_UPGRADE=hardlink permanently causes container startup
// failures on normal restarts (when versions already match).
//
// See docs/dev/postgresql-upgrade.md for details.
//
//go:embed postgres_startup.sh
var PostgresStartupScript string
