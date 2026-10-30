package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strconv"

	"ourway/server/models"
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
	// --- 2.4-8 · remote-control session recording.
	//
	// `RecordSessions` is the installation's recording policy: `off` (the default),
	// `on`, or `control-only` (record a session from the moment someone sends
	// control input). Recording a person's screen is a promise the installation
	// makes to the people whose screens it shows, so it is off unless the operator
	// says so. An unparseable value is not read as a policy: the server prints what
	// it received and runs `off`.
	RecordSessions string
	// RecordingsDir is where the frame-sequence files live. The frames are media,
	// not ledger rows, so they belong on a directory the operator sizes and backs
	// up, the way `INSTALLERS_DIR` is a directory the operator serves.
	RecordingsDir string
	// RecordingRetentionDays is how long a recording (row and file) is kept.
	// RECORDINGS_DIR is a disk that fills, and an unpruned delete method is not a
	// policy (M11's lesson, applied before it is needed).
	RecordingRetentionDays int
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
		ServerPort:             getEnv("SERVER_PORT", ":8080"),
		DatabaseURL:            getEnv("DATABASE_URL", "postgresql://postgres:ourway@localhost:5432/ourway?sslmode=disable"),
		JWTSecret:              jwtSecret,
		WSPath:                 getEnv("WS_PATH", "/ws"),
		RedisURL:               redisURL,
		RedisEnabled:           redisEnabled,
		WebURL:                 getEnv("WEB_URL", "http://localhost:3000"),
		InstallersDir:          getEnv("INSTALLERS_DIR", "./dist/agents"),
		MetricRetentionDays:    getEnvInt("METRIC_RETENTION_DAYS", 365),
		AuditRetentionDays:     getEnvInt("AUDIT_RETENTION_DAYS", 365),
		EnrollSecret:           getEnv("ENROLLMENT_SECRET", ""),
		WSOrigins:              getEnv("WS_ORIGINS", ""),
		RateLimitPerMinute:     getEnvInt("RATE_LIMIT_PER_MINUTE", 100),
		RateLimitBurst:         getEnvInt("RATE_LIMIT_BURST", 0),
		SMTPHost:               getEnv("SMTP_HOST", ""),
		SMTPPort:               getEnvInt("SMTP_PORT", 25),
		SMTPUsername:           getEnv("SMTP_USERNAME", ""),
		SMTPPassword:           getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:               getEnv("SMTP_FROM", ""),
		SMTPStartTLS:           getEnv("SMTP_STARTTLS", "false") == "true",
		RecordSessions:         recordingModeEnv(),
		RecordingsDir:          getEnv("RECORDINGS_DIR", "./dist/recordings"),
		RecordingRetentionDays: getEnvInt("RECORDING_RETENTION_DAYS", 30),
	}
}

// recordingModeEnv reads RECORD_SESSIONS and states what it did when the value is
// not one of the three policies the product implements. A typo here is the
// difference between "we record control sessions" and "we do not record, and
// nothing says so", so the answer is printed at boot and the safe mode is chosen:
// an unparseable value runs `off`, which records nothing, rather than the mode
// nobody typed.
func recordingModeEnv() string {
	raw := getEnv("RECORD_SESSIONS", models.RecordingModeOff)
	if !models.ValidRecordingMode(raw) {
		log.Printf("WARNING: RECORD_SESSIONS=%q is not a recording policy (off, on, control-only). "+
			"Running %q: this installation records NO remote-control sessions. "+
			"Fix the value and restart to record them.", raw, models.RecordingModeOff)
		return models.RecordingModeOff
	}
	if normalized := models.NormalizeRecordingMode(raw); normalized != raw {
		log.Printf("config: RECORD_SESSIONS=%q read as %q", raw, normalized)
		return normalized
	}
	return raw
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
