//go:build !windows

package selfupdate

import (
	"fmt"
	"runtime"
)

// restartService is a no-op on non-Windows platforms: the agent runs
// as a plain process or under an external supervisor (systemd,
// launchd), which handles restarts when this process exits.
func restartService() error {
	return fmt.Errorf("automatic restart not supported on %s; restart the agent manually", runtime.GOOS)
}
