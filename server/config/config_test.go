package config

import "testing"

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
