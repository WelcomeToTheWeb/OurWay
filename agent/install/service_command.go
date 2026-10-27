package install

import (
	"fmt"
	"strings"

	"ourway/agent/config"
)

// The program the Windows service runs, as the Service Control Manager wants
// it: the executable path, quoted when it contains spaces, followed by the
// agent's own arguments.
//
// The quoting rule is the SCM's, not a shell's: `lpBinaryPathName` is parsed by
// `CreateService`, which splits the string on spaces and treats quotes as
// grouping. An unquoted path with spaces becomes a program called `C:\Program`
// and the service dies at start (event log 1073/7000 style failures); a path
// quoted *and* followed by arguments is the only shape that runs.
func serviceBinPath(exe string, cfg *config.Config) string {
	path := exe
	if strings.Contains(path, " ") {
		path = `"` + path + `"`
	}
	return fmt.Sprintf(`%s --server %s --key %s`, path, cfg.ServerURL, cfg.DeviceKey)
}

// The binary must have a .exe name. `sc.exe` reports a binPath with no
// extension as an invalid name (1639) and the SCM refuses the service at start;
// a Windows binary named `ourway-agent` is not what an install produces anyway —
// the installer writes `ourway-agent.exe`. The case that fails is a hand-run
// `go build -o ourway-agent`, so name the fix.
func validateServiceBinary(exe string) error {
	if exe == "" {
		return fmt.Errorf("the agent binary path is empty")
	}
	if !strings.HasSuffix(strings.ToLower(exe), ".exe") {
		return fmt.Errorf(
			"the agent binary must be named with an .exe extension to run as a Windows service (got %q);\n"+
				"  install it as ourway-agent.exe, e.g. with ourway-installer", exe)
	}
	return nil
}
