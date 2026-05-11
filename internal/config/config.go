package config

import (
	"os"
	"strings"
)

type Config struct {
	DBDriver       string
	DBDSN          string
	Port           string
	BootstrapToken string
	JWTSecret      string
}

func Load() *Config {
	return &Config{
		DBDriver:       getEnv("CHICK_DB_DRIVER", "sqlite3"),
		DBDSN:          getEnv("CHICK_DB_DSN", "file:dev.db?_pragma=journal_mode(WAL)"),
		Port:           getEnv("CHICK_PORT", "8080"),
		BootstrapToken: getEnv("CHICK_BOOTSTRAP_TOKEN", ""),
		JWTSecret:      getEnv("CHICK_JWT_SECRET", ""),
	}
}

func (c *Config) UsePostgreSQL() bool {
	return strings.HasPrefix(c.DBDriver, "postgres")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
