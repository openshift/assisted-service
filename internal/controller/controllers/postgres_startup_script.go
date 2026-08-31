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

// PostgresHopScript upgrades exactly one PostgreSQL major version in an init
// container and then exits. Used for EUS skip-upgrades that sclorg cannot do
// in a single image (for example PG12 → PG15).
//
//go:embed postgres_hop.sh
var PostgresHopScript string
