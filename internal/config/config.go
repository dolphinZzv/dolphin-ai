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
	AllowedOrigins []string
	DevMode        bool
}

func Load() *Config {
	origins := getEnv("CHICK_ALLOWED_ORIGINS", "http://localhost:5173")
	return &Config{
		DBDriver:       getEnv("CHICK_DB_DRIVER", "sqlite3"),
		DBDSN:          getEnv("CHICK_DB_DSN", "file:dev.db?_pragma=journal_mode(WAL)"),
		Port:           getEnv("CHICK_PORT", "8080"),
		BootstrapToken: getEnv("CHICK_BOOTSTRAP_TOKEN", ""),
		JWTSecret:      getEnv("CHICK_JWT_SECRET", ""),
		AllowedOrigins: splitOrigins(origins),
		DevMode:        getEnv("CHICK_DEV_MODE", "") == "true",
	}
}

func splitOrigins(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, o := range strings.Split(s, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			result = append(result, o)
		}
	}
	return result
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
