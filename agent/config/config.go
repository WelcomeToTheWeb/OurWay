package config

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Version is the agent version. It is stamped at build time via
// -ldflags "-X ourway/agent/config.Version=x.y.z"; "1.0.0" is the
// fallback for development builds.
var Version = "1.0.0"

// Config holds the agent configuration.
type Config struct {
	ServerURL       string        `json:"server_url"`
	DeviceKey       string        `json:"device_key"`
	DeviceID        string        `json:"device_id"`
	Heartbeat       time.Duration `json:"heartbeat"`
	MetricsInterval time.Duration `json:"metrics_interval"`
	StreamInterval  time.Duration `json:"stream_interval"`
	LogFile         string        `json:"log_file"`
	Install         bool          `json:"-"`
	Uninstall       bool          `json:"-"`
	Yes             bool          `json:"-"` // --yes: no confirmation prompt
	Pause           bool          `json:"-"` // --pause: wait for Enter before exiting
	GrantInput      bool          `json:"-"` // --grant-input-permission: show the macOS Accessibility prompt
	ShowVersion     bool          `json:"-"`

	// ConfigPath is the file these values came from, empty when there was none.
	// Reported because there are now four places a value can come from, and an
	// operator looking at a running agent should not have to guess which answered.
	ConfigPath string `json:"-"`

	// Warnings are things noticed while reading the configuration that are not
	// fatal. They are printed by main, not swallowed here.
	Warnings []string `json:"-"`

	// KeyFromCommandLine says the device key arrived as an argument rather than
	// from the file. Everything about a service's command line is visible to any
	// account that can log in — `sc qc OurWayAgent` and `Get-CimInstance
	// Win32_Service` on Windows, the unit file and `systemctl cat` on Linux — so
	// for an installed agent the key was effectively readable by every local
	// account. It still works, and installs that pass it must keep working after
	// an upgrade, so it is warned about rather than refused.
	KeyFromCommandLine bool `json:"-"`
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

// Load reads the agent's configuration from four sources, in increasing order of
// precedence: the built-in defaults, a configuration file, the OURWAY_*
// environment variables, and the command line.
//
// The file is new, and it exists for one reason: until now the device key had
// nowhere else to live. A key passed to a service becomes part of the service's
// command line, and that is readable by any local account — `sc qc OurWayAgent`,
// `Get-CimInstance Win32_Service`, and on Linux the unit file itself. So an
// installed agent kept its wire credential where anyone who could log in could
// read it. The file sits beside the binary, where the installer can restrict it
// to SYSTEM and administrators.
//
// The precedence is chosen so that every existing install keeps working after an
// upgrade: they pass `--key`, and the command line still wins. What they get
// instead of a hard failure is a warning naming the exposure and the file that
// would end it, because bricking a running fleet to make a point about a
// credential is not the trade to make on somebody else's upgrade window.
func Load() (*Config, error) {
	return load(os.Args[1:], os.LookupEnv)
}

// load is Load with its inputs passed in, so the precedence can be tested without
// needing a process per case. It deliberately uses its own FlagSet: the previous
// version registered its flags on flag.CommandLine, which meant Load could run
// exactly once per process — the second call panicked on a redefined flag, and so
// none of this had ever been tested.
func load(args []string, lookupEnv func(string) (string, bool)) (*Config, error) {
	cfg := Default()

	fs := flag.NewFlagSet("ourway-agent", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		configPath = fs.String("config", "", "Configuration file (default: config/device.json beside the agent)")
		server     = fs.String("server", "", "Server WebSocket URL (default "+Default().ServerURL+")")
		key        = fs.String("key", "", "Device key")
		deviceID   = fs.String("device-id", "", "Device ID (optional; fetched from the server when empty)")
		install    = fs.Bool("install", false, "Install as a service and exit")
		uninstall  = fs.Bool("uninstall", false, "Uninstall the agent (service and files) and exit")
		yes        = fs.Bool("yes", false, "Do not ask for confirmation (with --uninstall)")
		pause      = fs.Bool("pause", false, "Wait for Enter before exiting (with --uninstall)")
		showVer    = fs.Bool("version", false, "Print version and exit")
		logFile    = fs.String("log-file", "", "Log file path")
		grantInput = fs.Bool("grant-input-permission", false, "Ask macOS to show the Accessibility grant prompt for remote input (macOS, cgo build) and exit")
	)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// 1. The file.
	path, err := resolveConfigPath(*configPath, lookupEnv)
	if err != nil {
		return nil, err
	}
	if path != "" {
		file, err := readConfigFile(path)
		if err != nil {
			return nil, err
		}
		cfg.ConfigPath = path
		file.applyTo(cfg)
		if cfg.DeviceKey != "" && runtime.GOOS != "windows" {
			if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
				cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
					"%s holds a device key and is mode %o: other accounts on this machine can read it. chmod 600 it.",
					path, fi.Mode().Perm()))
			}
		}
		// On Windows the file's protection is its NTFS ACL, which is not something
		// `os.Stat` can answer, so there is no check here that would be worth its
		// cost in surprises. The installer is the place that sets it, and it has to
		// say so rather than this file pretending the question is answered.
	}

	// 2. The environment.
	env := func(name string) string { v, _ := lookupEnv(name); return v }
	if v := env("OURWAY_SERVER"); v != "" {
		cfg.ServerURL = v
	}
	if v := env("OURWAY_DEVICE_KEY"); v != "" {
		cfg.DeviceKey = v
	}
	if v := env("OURWAY_DEVICE_ID"); v != "" {
		cfg.DeviceID = v
	}
	if v := env("OURWAY_LOG_FILE"); v != "" {
		cfg.LogFile = v
	}
	for _, e := range []struct {
		name   string
		target *time.Duration
	}{
		{"OURWAY_HEARTBEAT", &cfg.Heartbeat},
		{"OURWAY_METRICS_INTERVAL", &cfg.MetricsInterval},
	} {
		v := env(e.name)
		if v == "" {
			continue
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			// Reported, not obeyed, and not fatal — which is deliberately different
			// from how a bad config file is treated. Nothing has ever validated
			// these variables, so an installation may well be carrying one that a
			// human typed years ago and that every agent has ignored since. Refusing
			// to start over it would take agents offline to deliver a stricter
			// parser, so the agent says it out loud on every start instead.
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
				"%s=%q is not a duration (use a form like 30s); keeping %s", e.name, v, *e.target))
			continue
		}
		*e.target = d
	}

	// 3. The command line, which wins over everything.
	if *server != "" {
		cfg.ServerURL = *server
	}
	if *key != "" {
		cfg.DeviceKey = *key
	}
	if *deviceID != "" {
		cfg.DeviceID = *deviceID
	}
	if *logFile != "" {
		cfg.LogFile = *logFile
	}
	cfg.Install = *install
	cfg.Uninstall = *uninstall
	cfg.Yes = *yes
	cfg.Pause = *pause
	cfg.ShowVersion = *showVer
	cfg.GrantInput = *grantInput

	fs.Visit(func(f *flag.Flag) {
		if f.Name == "key" && *key != "" {
			cfg.KeyFromCommandLine = true
		}
	})
	if cfg.KeyFromCommandLine {
		cfg.Warnings = append(cfg.Warnings,
			"the device key was given on the command line, where the service manager shows it to any local "+
				"account (sc qc / Get-CimInstance on Windows, the unit file on Linux). Put it in "+
				FilePathHint()+" and drop --key from the service's path.")
	}

	return cfg, nil
}

// FilePathHint names where the key should live, for the operator-facing messages.
func FilePathHint() string {
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		return filepath.Join(filepath.Dir(exe), "config", "device.json")
	}
	return filepath.Join("config", "device.json")
}

// resolveConfigPath decides which file, if any, is read. An explicit source that
// cannot be read is an error rather than a shrug: an agent pointed at a file that
// is not there would fall back to ws://localhost:8080 with no key, fail to reach
// anything, and present itself to the console as an ordinary offline machine.
func resolveConfigPath(explicit string, lookupEnv func(string) (string, bool)) (string, error) {
	if explicit != "" {
		return explicit, requiredReadableFile(explicit, "--config")
	}
	if v, ok := lookupEnv("OURWAY_CONFIG"); ok && v != "" {
		return v, requiredReadableFile(v, "OURWAY_CONFIG")
	}
	for _, candidate := range defaultConfigPaths() {
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate, nil
		}
	}
	return "", nil
}

func requiredReadableFile(path, source string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s points at %s, which cannot be read: %w", source, path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("%s points at %s, which is a directory, not a configuration file", source, path)
	}
	return nil
}

// defaultConfigPaths are consulted when neither --config nor OURWAY_CONFIG names
// a file. The install directory comes from the executable's own location rather
// than a hardcoded path, because that is where the installer writes
// config/device.json on every platform — and it stays true for a custom
// --install-dir, which a hardcoded Program Files path would not.
func defaultConfigPaths() []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		out = append(out, filepath.Join(filepath.Dir(exe), "config", "device.json"))
	}
	if runtime.GOOS != "windows" {
		out = append(out, "/etc/ourway/device.json")
	}
	return out
}

// interval accepts either spelling an operator is likely to reach for: a Go
// duration string ("30s", the same form OURWAY_HEARTBEAT takes) or a bare number
// of seconds, which is exactly what the installer has written as
// "collect_interval": 30 since the first version. Accepting only one would mean
// an installed agent parses its own config file and then quietly ignores the
// number in it.
type interval time.Duration

func (i *interval) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "null" || text == "" {
		return nil
	}
	if strings.HasPrefix(text, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("%q is not a duration; use a form like \"30s\"", s)
		}
		*i = interval(d)
		return nil
	}
	var seconds float64
	if err := json.Unmarshal(data, &seconds); err != nil {
		return err
	}
	if seconds < 0 {
		return fmt.Errorf("an interval cannot be negative: %v seconds", seconds)
	}
	*i = interval(time.Duration(seconds * float64(time.Second)))
	return nil
}

// fileConfig is the shape of the file on disk. Pointers distinguish "absent" from
// "zero", so a file that names only a key does not reset the heartbeat to 0.
type fileConfig struct {
	ServerURL       string    `json:"server_url"`
	DeviceKey       string    `json:"device_key"`
	DeviceID        string    `json:"device_id"`
	LogFile         string    `json:"log_file"`
	Heartbeat       *interval `json:"heartbeat"`
	MetricsInterval *interval `json:"metrics_interval"`
	CollectInterval *interval `json:"collect_interval"`
	StreamInterval  *interval `json:"stream_interval"`
}

func readConfigFile(path string) (*fileConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("configuration file %s: %w", path, err)
	}
	// A leading UTF-8 BOM is accepted rather than rejected. PowerShell's
	// `Set-Content -Encoding utf8` and `Out-File`, and Notepad's "UTF-8 with BOM",
	// all write one — which is precisely how this file gets produced by the
	// deployment script of an installation that runs Windows. `json.Unmarshal` fails
	// on it with "invalid character 'ï'", describing a byte nobody can find in the
	// file they are looking at. This was not theoretical: the config file written by
	// PowerShell to verify this change is what produced that error.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var file fileConfig
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("configuration file %s is not readable as JSON: %w", path, err)
	}
	return &file, nil
}

func (f *fileConfig) applyTo(cfg *Config) {
	if f.ServerURL != "" {
		cfg.ServerURL = f.ServerURL
	}
	if f.DeviceKey != "" {
		cfg.DeviceKey = f.DeviceKey
	}
	if f.DeviceID != "" {
		cfg.DeviceID = f.DeviceID
	}
	if f.LogFile != "" {
		cfg.LogFile = f.LogFile
	}
	if f.Heartbeat != nil {
		cfg.Heartbeat = time.Duration(*f.Heartbeat)
	}
	// collect_interval is the installer's older name for the interval between
	// metric collections. It is honoured, and metrics_interval wins if a file
	// somehow carries both, so the name the agent itself uses is the authority.
	switch {
	case f.MetricsInterval != nil:
		cfg.MetricsInterval = time.Duration(*f.MetricsInterval)
	case f.CollectInterval != nil:
		cfg.MetricsInterval = time.Duration(*f.CollectInterval)
	}
	if f.StreamInterval != nil {
		cfg.StreamInterval = time.Duration(*f.StreamInterval)
	}
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

// Validate checks that the configuration is complete and valid. The sources are
// named in the order the loader consults them, because the message is the only
// documentation an operator standing in front of a refused start will read.
func (c *Config) Validate() error {
	if c.DeviceKey == "" {
		return fmt.Errorf("device key is required (set it in %s, or OURWAY_DEVICE_KEY, or --key)", FilePathHint())
	}
	if c.ServerURL == "" {
		return fmt.Errorf("server URL is required (set it in %s, or OURWAY_SERVER, or --server)", FilePathHint())
	}
	return nil
}
