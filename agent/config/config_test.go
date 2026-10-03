package config

import (
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Default()
	if cfg.ServerURL != "ws://localhost:8080" {
		t.Errorf("default server URL = %q", cfg.ServerURL)
	}
	if cfg.Heartbeat != 15*time.Second {
		t.Errorf("default heartbeat = %v, want 15s", cfg.Heartbeat)
	}
	if cfg.MetricsInterval != 60*time.Second {
		t.Errorf("default metrics interval = %v, want 60s", cfg.MetricsInterval)
	}
	if cfg.StreamInterval != 2*time.Second {
		t.Errorf("default stream interval = %v, want 2s", cfg.StreamInterval)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid",
			mutate:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "missing device key",
			mutate:  func(c *Config) { c.DeviceKey = "" },
			wantErr: true,
		},
		{
			name:    "missing server URL",
			mutate:  func(c *Config) { c.ServerURL = "" },
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.DeviceKey = "test-key"
			tt.mutate(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHTTPBaseURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"ws://localhost:8080/ws", "http://localhost:8080"},
		{"ws://localhost:8080", "http://localhost:8080"},
		{"wss://example.com", "https://example.com"},
		{"wss://example.com/ws", "https://example.com"},
		{"http://localhost:8080", "http://localhost:8080"},
		{"http://localhost:8080/ws", "http://localhost:8080"},
		{"https://example.com/", "https://example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := HTTPBaseURL(tt.in); got != tt.want {
				t.Errorf("HTTPBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("OURWAY_SERVER", "ws://prod.example.com")
	t.Setenv("OURWAY_DEVICE_KEY", "env-key")
	t.Setenv("OURWAY_HEARTBEAT", "45s")
	t.Setenv("OURWAY_METRICS_INTERVAL", "2m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ServerURL != "ws://prod.example.com" {
		t.Errorf("server URL from env = %q", cfg.ServerURL)
	}
	if cfg.DeviceKey != "env-key" {
		t.Errorf("device key from env = %q", cfg.DeviceKey)
	}
	if cfg.Heartbeat != 45*time.Second {
		t.Errorf("heartbeat from env = %v, want 45s", cfg.Heartbeat)
	}
	if cfg.MetricsInterval != 2*time.Minute {
		t.Errorf("metrics interval from env = %v, want 2m", cfg.MetricsInterval)
	}
}
