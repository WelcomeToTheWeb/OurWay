// Package selfupdate implements the agent's fully automatic update:
// compare the running build against the server's version, download the
// fresh agent and remote-control binaries when they differ, swap them
// in atomically, and restart the service.
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"ourway/agent/config"
)

const (
	// checkInterval is how often the agent polls when no push notification
	// arrived (the server also piggybacks its version on heartbeats).
	checkInterval = 1 * time.Hour
	// restartDelay lets logs flush before the process exits.
	restartDelay = 2 * time.Second
)

// Checker polls the server for version changes and applies updates.
type Checker struct {
	serverURL  string
	httpClient *http.Client
	stop       chan struct{}
	done       chan struct{}
}

// NewChecker creates a checker. serverURL is the agent's configured
// base URL (no trailing /ws).
func NewChecker(serverURL string) *Checker {
	return &Checker{
		serverURL: strings.TrimSuffix(serverURL, "/"),
		httpClient: &http.Client{Timeout: 60 * time.Second},
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// Run blocks until Stop is called, checking on start and then hourly.
func (c *Checker) Run() {
	defer close(c.done)
	c.check()
	t := time.NewTicker(checkInterval)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.check()
		}
	}
}

// CheckNow runs an immediate version check; the heartbeat push path
// calls it so an update applies within seconds of a server upgrade
// rather than waiting for the hourly poll.
func (c *Checker) CheckNow() {
	c.check()
}

// Stop terminates the check loop.
func (c *Checker) Stop() {
	close(c.stop)
	<-c.done
}

// check fetches the server version and self-updates when it differs
// from the running build. "dev" servers (unstamped local builds) never
// trigger an update: they carry no meaningful ordering.
func (c *Checker) check() {
	resp, err := c.httpClient.Get(c.serverURL + "/api/agent/version")
	if err != nil {
		log.Printf("selfupdate: version check failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("selfupdate: version check returned %d", resp.StatusCode)
		return
	}
	var v struct {
		Version   string `json:"version"`
		GitCommit string `json:"git_commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		log.Printf("selfupdate: bad version response: %v", err)
		return
	}
	if v.Version == "" || v.Version == "dev" || v.Version == config.Version {
		return
	}
	log.Printf("selfupdate: server has %s, running %s; updating", v.Version, config.Version)
	if err := c.apply(v.Version); err != nil {
		log.Printf("selfupdate: update to %s failed: %v", v.Version, err)
	}
}

// download fetches one of the server's installer artifacts (the same
// /api/v2/installers/ files the installer CLI uses) into dst.
func (c *Checker) download(dst string) (string, error) {
	name := filepath.Base(dst)
	// The staged names (ourway-agent.new / ourway-remote.exe.new) map to
	// the server's per-platform artifacts.
	var remote string
	switch {
	case strings.HasPrefix(name, "ourway-agent"):
		remote = fmt.Sprintf("ourway-agent-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext())
	case strings.HasPrefix(name, "ourway-remote"):
		remote = fmt.Sprintf("ourway-remote-%s-%s%s", runtime.GOOS, runtime.GOARCH, ext())
	default:
		return "", fmt.Errorf("unknown artifact %q", name)
	}
	resp, err := c.httpClient.Get(c.serverURL + "/api/v2/installers/" + remote)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		os.Remove(dst)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(dst)
		return "", err
	}
	// Never swap in a corrupt or truncated binary: the service would
	// restart into it. When the server publishes a checksum it must match.
	if want := c.publishedHash(remote); want != "" && !strings.EqualFold(want, hex.EncodeToString(h.Sum(nil))) {
		os.Remove(dst)
		return "", fmt.Errorf("checksum mismatch for %s", remote)
	}
	return dst, nil
}

// publishedHash returns the SHA-256 the server lists for an installer
// artifact, or "" when the list is unavailable or does not include it.
func (c *Checker) publishedHash(name string) string {
	resp, err := c.httpClient.Get(c.serverURL + "/api/v2/installers")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		Installers []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"installers"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return ""
	}
	for _, e := range body.Installers {
		if e.Name == name {
			return e.SHA256
		}
	}
	return ""
}

func ext() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// apply downloads the new agent and remote binaries and swaps them in.
// The old agent binary is renamed aside (Windows cannot delete a running
// exe but can rename it), the new one takes its place, and the service
// restart picks it up. The remote exe is replaced directly (it is not
// running during an update unless a session is active, in which case the
// rename still succeeds on Windows as long as it is not executing).
func (c *Checker) apply(serverVersion string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own path: %w", err)
	}
	dir := filepath.Dir(exe)

	// Download both binaries first: a partially applied update (new agent
	// but old remote exe) is worse than no update.
	agentTmp, err := c.download(filepath.Join(dir, "ourway-agent.new"))
	if err != nil {
		return fmt.Errorf("download agent: %w", err)
	}
	defer os.Remove(agentTmp)
	remoteTmp, rerr := c.download(filepath.Join(dir, "ourway-remote.exe.new"))
	if rerr != nil {
		// The remote exe is optional (Windows-only build); a 404 is fine.
		log.Printf("selfupdate: remote exe download skipped: %v", rerr)
	}

	// Swap the agent binary: rename the running exe aside, move the new
	// one into place, then restart via the service manager.
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("move current binary aside: %w", err)
	}
	if err := os.Rename(agentTmp, exe); err != nil {
		// Put the old binary back; the service must not restart into
		// nothing.
		os.Rename(old, exe)
		return fmt.Errorf("install new binary: %w", err)
	}
	os.Chmod(exe, 0o755)

	// Replace the remote exe when it was downloaded and the cached copy
	// exists (the agent re-downloads it on session start otherwise).
	remoteDst := filepath.Join(dir, "ourway-remote"+ext())
	if rerr == nil {
		if _, statErr := os.Stat(remoteDst); statErr == nil {
			if err := os.Rename(remoteTmp, remoteDst); err != nil {
				log.Printf("selfupdate: remote exe replace failed: %v", err)
				os.Remove(remoteTmp)
			}
		} else {
			os.Remove(remoteTmp)
		}
	}

	log.Printf("selfupdate: updated to %s; restarting", serverVersion)
	time.Sleep(restartDelay)
	return restartService()
}
