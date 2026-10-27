package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// serviceExecutable is the program the installed service must run.
//
// It used to be the literal `/usr/local/bin/ourway-agent`, which is only true
// for an agent that the installer placed there: run `./ourway-agent --install`
// from a directory you downloaded it into and systemd gets an ExecStart that
// does not exist. systemd reports that as `status=203/EXEC`, restarts it
// forever, and the console shows a unit that never comes back — which is how
// the 2.2-0 agent smoke job on Linux found it.
//
// The rule is the service runs the binary it was installed *from*. When that
// binary is somewhere a root-run service should not depend on, the installer
// says so: on macOS the Accessibility (TCC) grant follows the binary's path, so
// a service running out of a home directory loses control with the next
// self-update.
func serviceExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return "/usr/local/bin/ourway-agent"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if !servicePathIsStable(exe) {
		fmt.Printf("Warning: installing the service from %s.\n", exe)
		fmt.Println("  A service should run from a path the installer owns, e.g. /usr/local/bin/ourway-agent.")
		fmt.Println("  On macOS the Accessibility grant follows the binary's path, so a self-update to a new")
		fmt.Println("  location will leave remote control denied until Accessibility is granted again.")
	}
	return exe
}

func servicePathIsStable(exe string) bool {
	for _, stable := range []string{"/usr/local/bin/", "/usr/bin/", "/opt/"} {
		if strings.HasPrefix(exe, stable) {
			return true
		}
	}
	return false
}
