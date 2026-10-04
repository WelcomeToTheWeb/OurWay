//go:build windows

package session

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// remoteRunner manages the per-session remote-control executable: the
// ScreenConnect-style split where capture/input live in a dedicated
// process in the interactive user session instead of inside the RMM
// agent service.
type remoteRunner struct {
	mu    sync.Mutex
	proc  *os.Process
	stop  context.CancelFunc
	alive bool
}

var remote remoteRunner

// remoteBinPath is where the downloaded executable lives.
func remoteBinPath() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join(os.TempDir(), "ourway-remote.exe")
	}
	return filepath.Join(filepath.Dir(exe), "ourway-remote.exe")
}

// ensureRemoteBin downloads the remote-control executable from the
// server's installer endpoint if it is missing. The server serves
// /api/v2/installers/ourway-remote-windows-amd64.exe alongside the
// agent binaries.
func ensureRemoteBin(serverURL string) (string, error) {
	path := remoteBinPath()
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	url := fmt.Sprintf("%s/api/v2/installers/ourway-remote-%s-%s.exe", serverURL, "windows", runtime.GOARCH)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download remote exe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download remote exe: server returned %d", resp.StatusCode)
	}
	tmp := path + ".download"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", fmt.Errorf("create remote exe file: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("download remote exe: %w", err)
	}
	f.Close()
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("install remote exe: %w", err)
	}
	log.Printf("session: remote exe downloaded to %s", path)
	return path, nil
}

// StartRemoteSession downloads (if needed) and launches the
// remote-control executable for the given session. When it returns
// nil the exe is running and the caller must not run its own capture
// loop — the exe owns capture, input and frame upload for the session.
func StartRemoteSession(serverURL, deviceKey, sessionID string) error {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.alive {
		return nil
	}
	bin, err := ensureRemoteBin(serverURL)
	if err != nil {
		return err
	}
	cmdLine := fmt.Sprintf(`"%s" --server %s --key %s --session-id %s`, bin, serverURL, deviceKey, sessionID)
	pid, err := SpawnRemote(bin, cmdLine)
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find remote exe pid %d: %w", pid, err)
	}
	_, stop := context.WithCancel(context.Background())
	remote.proc = proc
	remote.stop = stop
	remote.alive = true
	log.Printf("session: remote exe spawned (pid %d) for session %s", pid, sessionID)
	go func() {
		_, err := proc.Wait()
		remote.mu.Lock()
		remote.alive = false
		remote.proc = nil
		remote.stop = nil
		remote.mu.Unlock()
		if err != nil {
			log.Printf("session: remote exe exited: %v", err)
		}
	}()
	return nil
}

// StopRemoteSession terminates the remote-control executable.
func StopRemoteSession() {
	remote.mu.Lock()
	proc := remote.proc
	remote.alive = false
	remote.proc = nil
	remote.stop = nil
	remote.mu.Unlock()
	if proc != nil {
		_ = proc.Kill()
	}
}

// startRemoteSession spawns the remote-control exe for the manager's
// current session. It returns true when the exe took over the session
// (the caller must not run its own capture loop), false when the split
// is unavailable and the legacy path should proceed.
func startRemoteSession(sm *SessionManager) bool {
	active, sessionID, serverURL, _ := sm.stateSnapshot()
	if !active || serverURL == "" || sessionID == "" {
		return false
	}
	if !runningInSession0() {
		// Already in the user session (interactive run): the in-process
		// path needs no split.
		return false
	}
	if err := StartRemoteSession(serverURL, sm.deviceKey, sessionID); err != nil {
		log.Printf("session: remote exe unavailable (%v); using in-process capture", err)
		return false
	}
	return true
}

// stopRemoteSession stops the exe if it is running (no-op otherwise).
func stopRemoteSession() {
	StopRemoteSession()
}
