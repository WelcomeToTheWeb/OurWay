//go:build windows

package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
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

// ensureRemoteBin makes sure the on-disk remote-control executable
// matches the one the server publishes at /api/v2/installers. It
// downloads when the file is missing or its SHA-256 differs from the
// server's, so a fixed exe reaches existing devices on their next
// session. If the server cannot be asked, an existing file is used.
// Callers must not invoke it while the exe is running (Windows will not
// replace a running image).
func ensureRemoteBin(serverURL string) (string, error) {
	path := remoteBinPath()
	name := fmt.Sprintf("ourway-remote-windows-%s.exe", runtime.GOARCH)

	want, listErr := serverInstallerHash(serverURL, name)
	have, haveErr := fileSHA256(path)
	if haveErr == nil && (listErr != nil || want == "" || strings.EqualFold(want, have)) {
		return path, nil
	}
	if listErr != nil && haveErr != nil {
		return "", fmt.Errorf("download remote exe: %w", listErr)
	}

	url := fmt.Sprintf("%s/api/v2/installers/%s", serverURL, name)
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
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("download remote exe: %w", err)
	}
	f.Close()
	if want != "" && !strings.EqualFold(want, hex.EncodeToString(h.Sum(nil))) {
		os.Remove(tmp)
		return "", fmt.Errorf("download remote exe: checksum mismatch")
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("install remote exe: %w", err)
	}
	log.Printf("session: remote exe installed at %s (sha256 %s)", path, hex.EncodeToString(h.Sum(nil)))
	return path, nil
}

// serverInstallerHash returns the SHA-256 the server publishes for the
// named installer artifact ("" when it is not listed).
func serverInstallerHash(serverURL, name string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(serverURL + "/api/v2/installers")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("installer list returned %d", resp.StatusCode)
	}
	var body struct {
		Installers []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"installers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode installer list: %w", err)
	}
	for _, e := range body.Installers {
		if e.Name == name {
			return e.SHA256, nil
		}
	}
	return "", nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// StartRemoteSession downloads (if needed) and launches the
// remote-control executable for the given session. When it returns
// nil the exe is running and the caller must not run its own capture
// loop — the exe owns capture, input and frame upload for the session.
func StartRemoteSession(serverURL, credential, sessionID string) error {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.alive {
		return nil
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
