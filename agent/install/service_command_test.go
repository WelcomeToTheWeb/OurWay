package install

import (
	"strings"
	"testing"

	"ourway/agent/config"
)

// TestServiceBinPathQuoting: the Service Control Manager splits lpBinaryPathName
// on spaces, so a path with spaces must be quoted and the agent's arguments
// must sit outside the quotes. An unquoted `C:\Program Files\...` is read as a
// program called `C:\Program`, and the service fails at start.
func TestServiceBinPathQuoting(t *testing.T) {
	cfg := &config.Config{ServerURL: "ws://example.test:9090/ws", DeviceKey: "key-123"}

	got := serviceBinPath(`C:\Program Files\OurWay\ourway-agent.exe`, cfg)
	want := `"C:\Program Files\OurWay\ourway-agent.exe" --server ws://example.test:9090/ws --key key-123`
	if got != want {
		t.Errorf("path with spaces:\n got: %s\nwant: %s", got, want)
	}

	got = serviceBinPath(`C:\ourway-agent.exe`, cfg)
	want = `C:\ourway-agent.exe --server ws://example.test:9090/ws --key key-123`
	if got != want {
		t.Errorf("path without spaces must not be quoted:\n got: %s\nwant: %s", got, want)
	}

	// The quoting groups the path only: the arguments are part of the value.
	if strings.Count(got, `"`) != 2 && strings.Count(serviceBinPath(`C:\Program Files\a.exe`, cfg), `"`) != 2 {
		t.Error("exactly the path is quoted, once on each side")
	}
}

// TestValidateServiceBinary: sc.exe and the SCM read a binPath with no
// extension as an invalid name, so the installer says so first.
func TestValidateServiceBinary(t *testing.T) {
	if err := validateServiceBinary(`C:\Program Files\OurWay\ourway-agent.exe`); err != nil {
		t.Errorf("a normal install path must be accepted, got: %v", err)
	}
	err := validateServiceBinary(`D:\a\OurWay\OurWay\ourway-agent`)
	if err == nil {
		t.Fatal("a binary with no .exe extension must be refused")
	}
	if !strings.Contains(err.Error(), ".exe") {
		t.Errorf("the message must name the fix, got: %v", err)
	}
}
