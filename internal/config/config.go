package config

import (
	"os"
	"strings"
)

type Config struct {
	DBDriver                string
	DBDSN                   string
	Port                    string
	JWTSecret               string
	AllowedOrigins          []string
	DevMode                 bool
	AllowHumanRegistration  bool
}

func Load() *Config {
	origins := getEnv("CHICK_ALLOWED_ORIGINS", "")
	return &Config{
		DBDriver:       getEnv("CHICK_DB_DRIVER", "sqlite3"),
		DBDSN:          getEnv("CHICK_DB_DSN", "file:dev.db?_pragma=journal_mode(WAL)"),
		Port:           getEnv("CHICK_PORT", "8080"),
		JWTSecret:      getEnv("CHICK_JWT_SECRET", ""),
		AllowedOrigins: splitOrigins(origins),
		DevMode:        getEnv("CHICK_DEV_MODE", "") == "true",
		AllowHumanRegistration: getEnv("CHICK_ALLOW_HUMAN_REGISTRATION", "false") == "true",
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
