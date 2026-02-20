package db

// Config holds the database connection configuration.
//
// SSLMode options and their security implications:
//   - "disable"     : No encryption (NOT recommended for production)
//   - "allow"       : Tries SSL, falls back to plaintext
//   - "prefer"      : Tries SSL, falls back to plaintext (default PostgreSQL behavior)
//   - "require"     : Encrypted connection required (DEFAULT - recommended minimum)
//   - "verify-ca"   : Encrypted + validates server cert against CA
//   - "verify-full" : Encrypted + validates server cert + hostname matching
//
// For production deployments with sensitive data, use "verify-ca" or "verify-full".
type Config struct {
	Host        string `envconfig:"DB_HOST"`
	Port        string `envconfig:"DB_PORT"`
	User        string `envconfig:"DB_USER"`
	Pass        string `envconfig:"DB_PASS"`
	Name        string `envconfig:"DB_NAME"`
	SSLMode     string `envconfig:"DB_SSLMODE" default:"require"`
	SSLRootCert string `envconfig:"DB_SSLROOTCERT"`
	SSLCert     string `envconfig:"DB_SSLCERT"`
	SSLKey      string `envconfig:"DB_SSLKEY"`
}
