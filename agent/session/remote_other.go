//go:build !windows && !linux && !darwin

package session

import (
	"fmt"
	"os"
)

func remoteSplitNeeded() bool { return false }

func spawnRemoteProc(bin string, args []string) (*os.Process, error) {
	return nil, fmt.Errorf("remote-control exe is not supported on this platform")
}
