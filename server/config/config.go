package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strconv"
)

// Version and GitCommit are stamped at build time via
// -ldflags "-X ourway/server/config.Version=x.y.z"; "dev" means an
// unstamped local build. Agents compare against it to decide whether a
// self-update is needed (fully automatic updates).
var (
	Version   = "dev"
	GitCommit = "unknown"
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
	// AuditRetentionDays is how long the audit ledger keeps rows
	// (AUDIT_RETENTION_DAYS, default 365). 2.3-6 states retention as a number
	// an operator can read, so the same value answers the ledger endpoint and
	// drives the sweeper that prunes it.
	AuditRetentionDays int
	// EnrollSecret, when non-empty, is required as X-Enrollment-Secret on
	// new device registration (re-registration of a known device with a
	// valid device key still works without it).
	EnrollSecret string
	// WSOrigins is the comma-separated list of allowed WebSocket origins
	// (C5). Empty falls back to WebURL (self-serve single-host installs).
	WSOrigins string
	// RateLimitPerMinute is the sustained per-user request budget the console
	// is designed to live inside. RateLimitBurst caps the instantaneous burst;
	// 0 means one tenth of the budget (the historical default).
	RateLimitPerMinute int
	RateLimitBurst     int
	// The mail transport for email notification channels (2.4-6). The installation
	// owns server, credentials and the sender address; recipients come from the
	// channel, so "client A's criticals to the on-call mailbox" is a channel
	// setting. SMTP_HOST empty means this installation cannot send mail, and the
	// API states that when an email channel is saved.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPStartTLS bool
}

// Load reads configuration from environment variables, applying defaults.
func Load() *Config {
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379/0")
	redisEnabled := getEnv("REDIS_ENABLED", "false") == "true"

	jwtSecret := getEnv("JWT_SECRET", "")
	if jwtSecret == "" {
		// No shared default secret: every boot gets a fresh random key.
		// Cost: sessions don't survive a server restart. Set JWT_SECRET
		// for sticky sessions.
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			log.Fatalf("config: generating JWT secret: %v", err)
		}
		jwtSecret = hex.EncodeToString(buf)
		log.Printf("WARNING: JWT_SECRET is not set; using an ephemeral random secret. Tokens will be invalidated on every server restart.")
	}

	return &Config{
		ServerPort:          getEnv("SERVER_PORT", ":8080"),
		DatabaseURL:         getEnv("DATABASE_URL", "postgresql://postgres:ourway@localhost:5432/ourway?sslmode=disable"),
		JWTSecret:           jwtSecret,
		WSPath:              getEnv("WS_PATH", "/ws"),
		RedisURL:            redisURL,
		RedisEnabled:        redisEnabled,
		WebURL:              getEnv("WEB_URL", "http://localhost:3000"),
		InstallersDir:       getEnv("INSTALLERS_DIR", "./dist/agents"),
		MetricRetentionDays: getEnvInt("METRIC_RETENTION_DAYS", 365),
		AuditRetentionDays:  getEnvInt("AUDIT_RETENTION_DAYS", 365),
		EnrollSecret:        getEnv("ENROLLMENT_SECRET", ""),
		WSOrigins:           getEnv("WS_ORIGINS", ""),
		RateLimitPerMinute:  getEnvInt("RATE_LIMIT_PER_MINUTE", 100),
		RateLimitBurst:      getEnvInt("RATE_LIMIT_BURST", 0),
		SMTPHost:            getEnv("SMTP_HOST", ""),
		SMTPPort:            getEnvInt("SMTP_PORT", 25),
		SMTPUsername:        getEnv("SMTP_USERNAME", ""),
		SMTPPassword:        getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:            getEnv("SMTP_FROM", ""),
		SMTPStartTLS:        getEnv("SMTP_STARTTLS", "false") == "true",
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
