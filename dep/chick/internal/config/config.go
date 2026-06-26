package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DBDriver                    string
	DBDSN                       string
	Port                        string
	JWTSecret                   string
	AllowedOrigins              []string
	DevMode                     bool
	AllowHumanRegistration      bool
	MCPAllowedCIDRs             []string
	DefaultRequirementProjectID uint
	PprofEnabled                bool
	RedisAddr                   string
	RedisPassword               string
	RedisDB                     int
}

func Load() *Config {
	origins := getEnv("CHICK_ALLOWED_ORIGINS", "")
	return &Config{
		DBDriver:                    getEnv("CHICK_DB_DRIVER", "sqlite3"),
		DBDSN:                       getEnv("CHICK_DB_DSN", "file:dev.db?_pragma=journal_mode(WAL)"),
		Port:                        getEnv("CHICK_PORT", "8080"),
		JWTSecret:                   getEnv("CHICK_JWT_SECRET", ""),
		AllowedOrigins:              splitOrigins(origins),
		DevMode:                     getEnv("CHICK_DEV_MODE", "") == "true",
		AllowHumanRegistration:      getEnv("CHICK_ALLOW_HUMAN_REGISTRATION", "false") == "true",
		MCPAllowedCIDRs:             splitCIDRs(getEnv("CHICK_MCP_ALLOWED_CIDRS", "")),
		DefaultRequirementProjectID: uint(getEnvInt("CHICK_REQUIREMENT_PROJECT_ID", 0)),
		PprofEnabled:  getEnv("CHICK_PPROF_ENABLED", "true") == "true",
		RedisAddr:     getEnv("CHICK_REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("CHICK_REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("CHICK_REDIS_DB", 1),
	}
}

func splitCIDRs(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			result = append(result, c)
		}
	}
	return result
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

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
