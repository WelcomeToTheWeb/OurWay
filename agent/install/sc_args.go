package install

import (
	"fmt"
	"strings"

	"ourway/agent/config"
)

// scCreateArgs builds the `sc.exe create` command line.
//
// sc.exe is picky about the equal-sign options, and the pickiness is invisible
// until it fails: every option name includes the equal sign, and any value
// containing a space has to arrive with the option *in the same argument*,
// quoted. Passing `binPath=` and the path as two arguments, or `display=` and
// `OurWay Agent` as two arguments, makes sc.exe print its usage and exit with
// 1639 (ERROR_INVALID_NAME) — which is what the 2.2-0 agent smoke job on the
// Windows runner found, six assertions after the agent had proven it can
// enroll, connect, heartbeat, report metrics and ship remote-control frames.
// The install path is the last thing the smoke checks, and it is the one the
// operator uses first.
// The binary must have a .exe name: sc.exe reads a binPath without one as an
// invalid name and answers 1639, which is the last thing anyone wants to
// debug about their service install. A real install gets this name from
// ourway-installer; the case that fails is a hand-run `go build -o
// ourway-agent`, so say what to do about it.
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

func scCreateArgs(service, exe string, cfg *config.Config) []string {
	binPath := fmt.Sprintf(`binPath= "%s" --server %s --key %s`, exe, cfg.ServerURL, cfg.DeviceKey)
	return []string{
		"create", service,
		binPath,
		"start= auto",
		`display= "OurWay Agent"`,
	}
}
