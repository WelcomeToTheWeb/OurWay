//go:build darwin

package session

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// spawnRemoteProc launches the remote-control exe inside the console
// user's login session via `launchctl asuser`, which is what lets
// screencapture reach the GUI. Screen Recording permission must be
// granted to the exe (System Settings > Privacy & Security).
func spawnRemoteProc(bin string, args []string) (*os.Process, error) {
	fi, err := os.Stat("/dev/console")
	if err != nil {
		return nil, fmt.Errorf("stat /dev/console: %w", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return nil, fmt.Errorf("no console user logged in")
	}
	cmd := exec.Command("/bin/launchctl", append([]string{"asuser", strconv.Itoa(int(st.Uid)), bin}, args...)...)
	return startRemoteCmd(cmd)
}
