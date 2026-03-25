package db

import (
	"fmt"
	"strings"
)

type Config struct {
	Host string `envconfig:"DB_HOST"`
	Port string `envconfig:"DB_PORT"`
	User string `envconfig:"DB_USER"`
	Pass string `envconfig:"DB_PASS"`
	Name string `envconfig:"DB_NAME"`
}

func escapeConninfoValue(v string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + replacer.Replace(v) + "'"
}

// LibpqDSN returns a PostgreSQL libpq keyword connection string for GORM/pgx.
// client_encoding=UTF8 is always set so pgx v5 simple-query paths (used by some GORM APIs) work regardless of server defaults.
func LibpqDSN(host, port, user, password, database string) string {
	s := fmt.Sprintf("host=%s port=%s user=%s password=%s sslmode=disable client_encoding=UTF8",
		host, port, escapeConninfoValue(user), escapeConninfoValue(password))
	if database != "" {
		s += fmt.Sprintf(" database=%s", escapeConninfoValue(database))
	}
	return s
}

// LibpqDSN builds a libpq DSN from c using the same rules as the package function.
func (c Config) LibpqDSN() string {
	return LibpqDSN(c.Host, c.Port, c.User, c.Pass, c.Name)
}
