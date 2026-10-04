//go:build windows

package selfupdate

import (
	"fmt"
	"os/exec"
	"syscall"
)

// restartService reloads the updated binary: the current process is the
// OurWayAgent service itself, so it cannot stop its own service (the SCM
// would kill it mid-call). Instead a detached cmd.exe is scheduled to
// restart the service shortly after this process exits; the service's
// recovery actions (restart/5000/restart/10000/restart/30000) are the
// safety net if the scheduled restart fails.
func restartService() error {
	script := `timeout /t 3 /nobreak >nul & sc.exe stop OurWayAgent & sc.exe start OurWayAgent`
	cmd := exec.Command("cmd.exe", "/c", script)
	// CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS: the helper must
	// outlive this process, which the SCM is about to stop.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("schedule service restart: %w", err)
	}
	// Do not wait: the helper's first act is to stop this very service.
	return nil
}
