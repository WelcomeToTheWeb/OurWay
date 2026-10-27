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

// scCreateCommandLine composes the command line exactly as an operator would
// type it in cmd.exe.
//
// sc.exe parses the *raw* command line for its equal-sign options: it looks for
// a token beginning `binPath=`, and a token beginning `display=`. Go's
// exec.Command quotes any argument containing spaces — which turns the token
// into `"binPath= ..."` — and sc.exe no longer recognises the option at all.
// That is why the installer must hand CreateProcess the line verbatim
// (SysProcAttr.CmdLine): the two shapes sc.exe accepts, and the shapes Go will
// build for you, are not the same strings.
func scCreateCommandLine(service, exe string, cfg *config.Config) string {
	return "sc.exe " + strings.Join(scCreateArgs(service, exe, cfg), " ")
}

func scCreateArgs(service, exe string, cfg *config.Config) []string {
	// The whole value is quoted, not just the path. sc.exe takes everything
	// after `binPath=` as the service's command line, and quotes the path alone
	// (Microsoft's own example, for a path with spaces) leaves the agent's
	// arguments outside the quoting — the service ends up registered with a
	// broken image path and 1639 is the polite answer.
	binPath := fmt.Sprintf(`binPath= "%s --server %s --key %s"`, exe, cfg.ServerURL, cfg.DeviceKey)
	return []string{
		"create", service,
		binPath,
		"start= auto",
		`display= "OurWay Agent"`,
	}
}
