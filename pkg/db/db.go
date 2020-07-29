package db

type Config struct {
	Host string `envconfig:"DB_HOST" default:"postgres"`
	Port string `envconfig:"DB_PORT" default:"5432"`
	User string `envconfig:"DB_USER" default:"admin"`
	Pass string `envconfig:"DB_PASS" default:"admin"`
	Name string `envconfig:"DB_NAME" default:"installer"`
}
