package config

import (
	"testing"

	"ourway/server/models"
)

func TestLoadRateLimitDefaults(t *testing.T) {
	// Unset means the historical budget: 100/min, burst derived as a tenth.
	cfg := Load()
	if cfg.RateLimitPerMinute != 100 {
		t.Fatalf("RATE_LIMIT_PER_MINUTE unset: got %d, want 100", cfg.RateLimitPerMinute)
	}
	if cfg.RateLimitBurst != 0 {
		t.Fatalf("RATE_LIMIT_BURST unset: got %d, want 0 (derive from the budget)", cfg.RateLimitBurst)
	}
}

func TestLoadRateLimitFromEnvironment(t *testing.T) {
	t.Setenv("RATE_LIMIT_PER_MINUTE", "600")
	t.Setenv("RATE_LIMIT_BURST", "60")
	cfg := Load()
	if cfg.RateLimitPerMinute != 600 {
		t.Fatalf("RATE_LIMIT_PER_MINUTE=600: got %d", cfg.RateLimitPerMinute)
	}
	if cfg.RateLimitBurst != 60 {
		t.Fatalf("RATE_LIMIT_BURST=60: got %d", cfg.RateLimitBurst)
	}
}

// 2.4-8 · `RECORD_SESSIONS` is the installation's promise to record remote-control
// sessions. The default is `off`, an unparseable value must never be read as an
// enabled policy, and `control_only` is accepted as the spelling people type for
// `control-only` and normalized — so the console and the recorder read one value.
func TestLoadRecordingPolicyDefaultsToOff(t *testing.T) {
	cfg := Load()
	if cfg.RecordSessions != models.RecordingModeOff {
		t.Fatalf("RECORD_SESSIONS unset: got %q, want off", cfg.RecordSessions)
	}
	if cfg.RecordingRetentionDays != 30 {
		t.Fatalf("RECORDING_RETENTION_DAYS unset: got %d, want 30", cfg.RecordingRetentionDays)
	}
	if cfg.RecordingsDir == "" {
		t.Fatal("RECORDINGS_DIR unset left no directory")
	}
}

func TestLoadRecordingPolicyFromEnvironment(t *testing.T) {
	t.Setenv("RECORD_SESSIONS", models.RecordingModeControl)
	t.Setenv("RECORDINGS_DIR", "/var/lib/ourway/recordings")
	t.Setenv("RECORDING_RETENTION_DAYS", "90")
	cfg := Load()
	if cfg.RecordSessions != models.RecordingModeControl {
		t.Fatalf("RECORD_SESSIONS=control-only: got %q", cfg.RecordSessions)
	}
	if cfg.RecordingsDir != "/var/lib/ourway/recordings" {
		t.Fatalf("RECORDINGS_DIR: got %q", cfg.RecordingsDir)
	}
	if cfg.RecordingRetentionDays != 90 {
		t.Fatalf("RECORDING_RETENTION_DAYS=90: got %d", cfg.RecordingRetentionDays)
	}
}

func TestLoadRecordingPolicyAcceptsTheUnderscoreSpelling(t *testing.T) {
	t.Setenv("RECORD_SESSIONS", models.RecordingModeControlLo)
	cfg := Load()
	if cfg.RecordSessions != models.RecordingModeControl {
		t.Fatalf("RECORD_SESSIONS=control_only: got %q, want the normalized control-only", cfg.RecordSessions)
	}
}

// A typo in this variable is the difference between "we record control sessions"
// and "we record nothing, and nothing says so". The safe reading is chosen, and
// the server prints what it received.
func TestLoadRecordingPolicyRefusesToGuessAnUnreadableValue(t *testing.T) {
	t.Setenv("RECORD_SESSIONS", "yes-please")
	cfg := Load()
	if cfg.RecordSessions != models.RecordingModeOff {
		t.Fatalf("RECORD_SESSIONS=yes-please: got %q, want off", cfg.RecordSessions)
	}
}
