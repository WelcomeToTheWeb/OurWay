package install

import (
	"fmt"

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
func scCreateArgs(service, exe string, cfg *config.Config) []string {
	binPath := fmt.Sprintf(`binPath= "%s" --server %s --key %s`, exe, cfg.ServerURL, cfg.DeviceKey)
	return []string{
		"create", service,
		binPath,
		"start= auto",
		`display= "OurWay Agent"`,
	}
}
