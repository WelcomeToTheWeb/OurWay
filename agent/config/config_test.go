package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The precedence this package implements had never been asserted, because it could
// not be: `Load` registered its flags on flag.CommandLine, so a second call in the
// same process panicked on a redefined flag, and varying the sources requires at
// least one call per case. `load` takes the arguments and the environment as
// parameters, so all four sources can be varied inside one test binary.

func envWith(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	}
}

func writeConfig(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

// These four tests are the package's existing ones. They are kept deliberately:
// `TestLoadFromEnv` is the only reason the old `Load` was ever exercised at all, and
// it could only be called once per process, so nothing could vary the sources against
// each other. It calls `load(nil, os.LookupEnv)` rather than `Load()` for one reason:
// `Load` now reads os.Args, and inside a test binary those are the `-test.*` flags,
// which a FlagSet that does not know them is correct to refuse. What the test asserts
// — that the environment is obeyed — is unchanged.

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

	cfg, err := load(nil, os.LookupEnv)
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

func TestFileBeatsTheDefaults(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "device.json", `{
		"server_url": "wss://rmm.example.com/ws",
		"device_key": "dk_from_file",
		"device_id": "dev_from_file"
	}`, 0o600)

	cfg, err := load([]string{"--config", path}, envWith(nil))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ServerURL != "wss://rmm.example.com/ws" || cfg.DeviceKey != "dk_from_file" || cfg.DeviceID != "dev_from_file" {
		t.Errorf("the file was not applied: %+v", cfg)
	}
	if cfg.ConfigPath != path {
		t.Errorf("ConfigPath = %q, want %q — an operator has to be able to see which file answered", cfg.ConfigPath, path)
	}
	if cfg.KeyFromCommandLine {
		t.Error("a key that came from the file is reported as coming from the command line")
	}
}

func TestEnvironmentBeatsTheFile(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "device.json", `{"server_url":"wss://from-file/ws","device_key":"dk_file"}`, 0o600)

	cfg, err := load([]string{"--config", path}, envWith(map[string]string{
		"OURWAY_SERVER":     "wss://from-env/ws",
		"OURWAY_DEVICE_KEY": "dk_env",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ServerURL != "wss://from-env/ws" || cfg.DeviceKey != "dk_env" {
		t.Errorf("the environment did not win over the file: %+v", cfg)
	}
}

func TestCommandLineBeatsTheEnvironmentAndTheFile(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "device.json", `{"server_url":"wss://from-file/ws","device_key":"dk_file"}`, 0o600)

	cfg, err := load([]string{"--config", path, "--server", "wss://from-argv/ws", "--key", "dk_argv"}, envWith(map[string]string{
		"OURWAY_SERVER":     "wss://from-env/ws",
		"OURWAY_DEVICE_KEY": "dk_env",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ServerURL != "wss://from-argv/ws" || cfg.DeviceKey != "dk_argv" {
		t.Errorf("the command line did not win: %+v", cfg)
	}
}

// An installed agent passes --server and --key on the service's command line. It
// must keep working after this change — the alternative is an upgrade that takes
// the fleet offline — but it must also stop pretending the key is private.
func TestAnExistingInstallWithoutAFileStillRunsAndSaysWhatIsExposed(t *testing.T) {
	cfg, err := load([]string{"--server", "wss://rmm.example.com/ws", "--key", "dk_installed"}, envWith(nil))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an existing install's arguments no longer produce a valid config: %v", err)
	}
	if !cfg.KeyFromCommandLine {
		t.Fatal("the exposure was not detected, so nothing will be reported to the operator")
	}
	var said string
	for _, w := range cfg.Warnings {
		said += w + "\n"
	}
	if !strings.Contains(said, "sc qc") {
		t.Errorf("the warning does not name how the key is exposed:\n%s", said)
	}
	if !strings.Contains(said, "device.json") {
		t.Errorf("the warning does not name the file that would end the exposure:\n%s", said)
	}
}

// A file the operator pointed at explicitly has to be there. Falling back to
// defaults instead would produce an agent aimed at ws://localhost:8080 with no
// key, which looks from the console exactly like a machine that is offline.
func TestAnExplicitConfigFileThatCannotBeReadIsFatal(t *testing.T) {
	for _, tc := range []struct{ name, arg, want string }{
		{"missing", filepath.Join(t.TempDir(), "absent.json"), "cannot be read"},
		{"directory", t.TempDir(), "not a configuration file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load([]string{"--config", tc.arg}, envWith(nil))
			if err == nil {
				t.Fatalf("--config %s was accepted; the agent would have started on defaults", tc.arg)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "--config") {
				t.Errorf("error does not name the source and the reason (%q): %v", tc.want, err)
			}
		})
	}
}

func TestOURWAY_CONFIGIsHonouredAndMustExist(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "other.json", `{"device_key":"dk_via_env_path"}`, 0o600)

	cfg, err := load(nil, envWith(map[string]string{"OURWAY_CONFIG": path}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DeviceKey != "dk_via_env_path" {
		t.Errorf("OURWAY_CONFIG was not read: %+v", cfg)
	}

	if _, err := load(nil, envWith(map[string]string{"OURWAY_CONFIG": filepath.Join(dir, "gone.json")})); err == nil {
		t.Error("OURWAY_CONFIG pointing at nothing was accepted")
	}
}

func TestAMalformedConfigFileNamesItself(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "device.json", `{"server_url": "https://x", `, 0o600)

	_, err := load([]string{"--config", path}, envWith(nil))
	if err == nil {
		t.Fatal("a truncated JSON file was accepted")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the error does not name the file an operator has to go and fix: %v", err)
	}
}

// The installer has always written "collect_interval": 30 — a bare number of
// seconds, under a name the agent's own struct never used. Reading the file and
// ignoring that number is precisely the decorative-config-file failure this change
// is meant to end, so both spellings are pinned.
func TestTheInstallersOwnFileShapeIsActuallyRead(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "device.json", `{
		"server_url": "http://localhost:8080",
		"device_key": "dk_installed",
		"collect_interval": 30
	}`, 0o600)

	cfg, err := load([]string{"--config", path}, envWith(nil))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MetricsInterval != 30*time.Second {
		t.Errorf("collect_interval: 30 produced %s, want 30s", cfg.MetricsInterval)
	}

	both := writeConfig(t, dir, "both.json", `{"collect_interval": 30, "metrics_interval": "45s"}`, 0o600)
	cfg, err = load([]string{"--config", both}, envWith(nil))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MetricsInterval != 45*time.Second {
		t.Errorf("metrics_interval did not win over the older collect_interval: %s", cfg.MetricsInterval)
	}

	bad := writeConfig(t, dir, "bad.json", `{"heartbeat": "ten seconds"}`, 0o600)
	if _, err := load([]string{"--config", bad}, envWith(nil)); err == nil {
		t.Error("a duration nobody can parse was accepted")
	}
}

// A partial file must add to the defaults, not reset the fields it omits: a file
// carrying only a key must not silently turn a 15-second heartbeat into zero.
func TestAPartialFileLeavesTheRestAlone(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "device.json", `{"device_key":"dk_only"}`, 0o600)

	cfg, err := load([]string{"--config", path}, envWith(nil))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := Default()
	if cfg.Heartbeat != want.Heartbeat || cfg.MetricsInterval != want.MetricsInterval || cfg.StreamInterval != want.StreamInterval {
		t.Errorf("omitted intervals were reset: %+v", cfg)
	}
	if cfg.ServerURL != want.ServerURL {
		t.Errorf("omitted server_url was reset to %q, want the default %q", cfg.ServerURL, want.ServerURL)
	}
}

func TestABrokenEnvironmentDurationIsReportedNotObeyed(t *testing.T) {
	cfg, err := load(nil, envWith(map[string]string{"OURWAY_HEARTBEAT": "banana"}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Heartbeat != Default().Heartbeat {
		t.Errorf("an unparseable heartbeat changed the interval to %s", cfg.Heartbeat)
	}
	found := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "OURWAY_HEARTBEAT") {
			found = true
		}
	}
	if !found {
		t.Errorf("the unparseable variable was ignored silently: %v", cfg.Warnings)
	}
}

// The installer writes config/device.json inside the install directory, which is
// beside the binary. That is the layout a real install produces, so it is the
// layout the default lookup has to satisfy — including for a custom --install-dir,
// which a hardcoded path would not.
func TestTheFileBesideTheExecutableIsFoundWithoutBeingNamed(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate the test binary: %v", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Join(filepath.Dir(exe), "config")
	created := false
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Skipf("cannot create %s: %v", dir, err)
		}
		created = true
	}
	path := writeConfig(t, dir, "device.json", `{"device_key":"dk_beside_the_binary"}`, 0o600)
	if created {
		t.Cleanup(func() { _ = os.Remove(dir) })
	}

	cfg, err := load(nil, envWith(nil))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DeviceKey != "dk_beside_the_binary" || cfg.ConfigPath != path {
		t.Errorf("the installer's own layout was not found: path=%q key=%q", cfg.ConfigPath, cfg.DeviceKey)
	}
}

func TestAKeyFileOtherAccountsCanReadIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Unix permission bits the test sets are not what protects a file on Windows; there the ACL is, and os.Stat cannot read it")
	}
	dir := t.TempDir()
	path := writeConfig(t, dir, "device.json", `{"device_key":"dk_exposed"}`, 0o644)

	cfg, err := load([]string{"--config", path}, envWith(nil))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	found := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "chmod 600") {
			found = true
		}
	}
	if !found {
		t.Errorf("a key readable by every account on the machine was not reported: %v", cfg.Warnings)
	}
}

// A file generated on Windows by PowerShell or Notepad carries a UTF-8 BOM, and
// the failure mode is an agent that will not start and complains about a character
// that is not visible in the file. Pinned because it was found by accident: the
// PowerShell command used to check this behaviour by hand is what wrote the BOM.
func TestAFileWrittenWithABOMStillLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.json")
	body := "\xEF\xBB\xBF" + `{"server_url":"wss://rmm.example.com/ws","device_key":"dk_bommed","collect_interval":30}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	cfg, err := load([]string{"--config", path}, envWith(nil))
	if err != nil {
		t.Fatalf("a BOM stopped the agent from starting: %v", err)
	}
	if cfg.DeviceKey != "dk_bommed" || cfg.MetricsInterval != 30*time.Second {
		t.Errorf("the file after the BOM was not applied: %+v", cfg)
	}
}

func TestValidateNamesTheFileFirst(t *testing.T) {
	err := (&Config{}).Validate()
	if err == nil || !strings.Contains(err.Error(), "device.json") {
		t.Errorf("a missing key does not point at the file where it belongs: %v", err)
	}
}
