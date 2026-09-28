package config

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Version is the agent version.
const Version = "1.0.0"

// Config holds the agent configuration.
type Config struct {
	ServerURL       string        `json:"server_url"`
	DeviceKey       string        `json:"device_key"`
	Heartbeat       time.Duration `json:"heartbeat"`
	MetricsInterval time.Duration `json:"metrics_interval"`
	StreamInterval  time.Duration `json:"stream_interval"`
	LogFile         string        `json:"log_file"`
	Install         bool          `json:"-"`
	Uninstall       bool          `json:"-"`
	ShowVersion     bool          `json:"-"`
}

// Default returns a Config with default values.
func Default() *Config {
	return &Config{
		ServerURL:       "ws://localhost:8080",
		Heartbeat:       15 * time.Second,
		MetricsInterval: 60 * time.Second,
		StreamInterval:  2 * time.Second,
	}
}

// Load reads configuration from environment variables and command-line flags.
// CLI flags override environment variables, which override defaults.
func Load() (*Config, error) {
	cfg := Default()

	// Load from environment variables
	if v := os.Getenv("OURWAY_SERVER"); v != "" {
		cfg.ServerURL = v
	}
	if v := os.Getenv("OURWAY_DEVICE_KEY"); v != "" {
		cfg.DeviceKey = v
	}
	if v := os.Getenv("OURWAY_HEARTBEAT"); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			cfg.Heartbeat = d
		}
	}
	if v := os.Getenv("OURWAY_METRICS_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			cfg.MetricsInterval = d
		}
	}

	// Parse command-line flags (override env vars)
	server := flag.String("server", cfg.ServerURL, "Server WebSocket URL")
	key := flag.String("key", cfg.DeviceKey, "Device key")
	install := flag.Bool("install", false, "Install as a service and exit")
	uninstall := flag.Bool("uninstall", false, "Uninstall service and exit")
	version := flag.Bool("version", false, "Print version and exit")
	logFile := flag.String("log-file", "", "Log file path")
	flag.Parse()

	cfg.ServerURL = *server
	cfg.DeviceKey = *key
	cfg.Install = *install
	cfg.Uninstall = *uninstall
	cfg.ShowVersion = *version
	cfg.LogFile = *logFile

	return cfg, nil
}

// HTTPBaseURL converts a WebSocket server URL (e.g. ws://host:8080/ws)
// into the HTTP base URL (http://host:8080) used for the agent's REST
// calls. Plain http(s) URLs are normalized by stripping a trailing /ws
// path and slash, so the result is safe to call repeatedly.
func HTTPBaseURL(serverURL string) string {
	u := serverURL
	switch {
	case strings.HasPrefix(u, "wss://"):
		u = "https://" + strings.TrimPrefix(u, "wss://")
	case strings.HasPrefix(u, "ws://"):
		u = "http://" + strings.TrimPrefix(u, "ws://")
	}
	u = strings.TrimSuffix(u, "/ws")
	return strings.TrimSuffix(u, "/")
}

// Validate checks that the configuration is complete and valid.
func (c *Config) Validate() error {
	if c.DeviceKey == "" {
		return fmt.Errorf("device key is required (set --key or OURWAY_DEVICE_KEY)")
	}
	if c.ServerURL == "" {
		return fmt.Errorf("server URL is required (set --server or OURWAY_SERVER)")
	}
	return nil
}
