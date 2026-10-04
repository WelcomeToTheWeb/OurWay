//go:build windows

package session

import (
	"fmt"
	"os"
	"strings"
)

// remoteSplitNeeded reports whether the exe must be spawned into the
// user's session: true when the agent runs as the Session 0 service.
func remoteSplitNeeded() bool { return runningInSession0() }

// spawnRemoteProc launches the remote-control exe in the interactive
// session (see SpawnRemote) and returns a handle to supervise it.
func spawnRemoteProc(bin string, args []string) (*os.Process, error) {
	cmdLine := `"` + bin + `"`
	for _, a := range args {
		cmdLine += " " + quoteWinArg(a)
	}
	pid, err := SpawnRemote(bin, cmdLine)
	if err != nil {
		return nil, err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("find remote exe pid %d: %w", pid, err)
	}
	return proc, nil
}

func quoteWinArg(a string) string {
	if a != "" && !strings.ContainsAny(a, " \t\"") {
		return a
	}
	return `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
}
