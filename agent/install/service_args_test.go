package install

import (
	"strings"
	"testing"

	"ourway/agent/config"
)

// TestSCCreateArgsQuoting pins the shape `sc.exe create` accepts.
//
// sc.exe's options include the equal sign in the name, and a value with a
// space is only parsed as that option's value when it travels with the option
// in one argument. Split them and sc.exe prints its usage and fails 1639 —
// the Windows runner found this at the last smoke assertion, after the agent
// had already proven it enrolls, connects, heartbeats, reports metrics and
// ships remote frames.
func TestSCCreateArgsQuoting(t *testing.T) {
	cfg := &config.Config{ServerURL: "ws://example.test:9090/ws", DeviceKey: "key-123"}
	args := scCreateArgs("OurWayAgent", `C:\Program Files\OurWay\ourway-agent.exe`, cfg)

	joined := strings.Join(args, " ")
	for _, want := range []string{
		// The path is quoted because it contains a space; the agent's own arguments
		// follow it, all of them inside the one `binPath=` option.
		`binPath= "C:\Program Files\OurWay\ourway-agent.exe --server ws://example.test:9090/ws --key key-123"`,
		`start= auto`,
		`display= "OurWay Agent"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("sc.exe create line must contain %q, got: %s", want, joined)
		}
	}

	// Every option that carries a value with a space is one argument, not two.
	for _, arg := range args {
		if strings.HasSuffix(arg, "=") {
			t.Errorf("an option name was passed as its own argument: %q", arg)
		}
		if strings.Contains(arg, " ") && !strings.Contains(arg, "=") && !strings.HasPrefix(arg, "create") {
			t.Errorf("a bare value with a space travels on its own: %q", arg)
		}
	}
}

// TestValidateServiceBinary: sc.exe reads a binPath with no extension as an
// invalid service name and fails 1639, so the installer says so first.
func TestValidateServiceBinary(t *testing.T) {
	if err := validateServiceBinary(`C:\Program Files\OurWay\ourway-agent.exe`); err != nil {
		t.Errorf("a normal install path must be accepted, got: %v", err)
	}
	err := validateServiceBinary(`D:\a\OurWay\OurWay\ourway-agent`)
	if err == nil {
		t.Fatal("a binary with no .exe extension must be refused: sc.exe fails it with 1639")
	}
	if !strings.Contains(err.Error(), ".exe") {
		t.Errorf("the message must name the fix, got: %v", err)
	}
}
