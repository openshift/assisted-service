package db

type Config struct {
	Host string `envconfig:"DB_HOST"`
	Port string `envconfig:"DB_PORT"`
	User string `envconfig:"DB_USER"`
	Pass string `envconfig:"DB_PASS"`
	Name string `envconfig:"DB_NAME"`
}
