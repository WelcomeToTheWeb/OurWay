package config

import (
	"os"
	"strconv"
)

// Config holds all server configuration loaded from environment variables.
type Config struct {
	ServerPort          string
	DatabaseURL         string
	JWTSecret           string
	WSPath              string
	RedisURL            string
	RedisEnabled        bool
	WebURL              string
	InstallersDir       string
	MetricRetentionDays int
}

// Load reads configuration from environment variables, applying defaults.
func Load() *Config {
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379/0")
	redisEnabled := getEnv("REDIS_ENABLED", "false") == "true"

	return &Config{
		ServerPort:          getEnv("SERVER_PORT", ":8080"),
		DatabaseURL:         getEnv("DATABASE_URL", "postgresql://postgres:ourway@localhost:5432/ourway?sslmode=disable"),
		JWTSecret:           getEnv("JWT_SECRET", "ourway-secret-key"),
		WSPath:              getEnv("WS_PATH", "/ws"),
		RedisURL:            redisURL,
		RedisEnabled:        redisEnabled,
		WebURL:              getEnv("WEB_URL", "http://localhost:3000"),
		InstallersDir:       getEnv("INSTALLERS_DIR", "./dist/agents"),
		MetricRetentionDays: getEnvInt("METRIC_RETENTION_DAYS", 365),
	}
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
