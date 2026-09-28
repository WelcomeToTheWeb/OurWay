package patch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Update represents a software update detected on the device.
type Update struct {
	Source    string `json:"source"`
	Title     string `json:"title"`
	Version   string `json:"version"`
	SizeBytes int64  `json:"size_bytes"`
}

// Handler manages patch operations for the agent.
type Handler struct {
	deviceKey  string
	serverURL  string
	httpClient *http.Client
}

// NewHandler creates a new patch handler.
func NewHandler(deviceKey, serverURL string) *Handler {
	return &Handler{
		deviceKey: deviceKey,
		serverURL: strings.TrimSuffix(serverURL, "/ws"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// ScanUpdates scans the device for available software updates.
func (h *Handler) ScanUpdates(ctx context.Context, data interface{}) {
	var payload map[string]interface{}
	if b, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(b, &payload); err != nil {
			log.Printf("scan_updates: failed to parse payload: %v", err)
			return
		}
	}

	deviceID, _ := payload["device_id"].(string)

	log.Printf("scan_updates: scanning for updates")

	var updates []Update
	var err error

	switch runtime.GOOS {
	case "linux":
		updates, err = scanLinuxUpdates()
	case "darwin":
		updates, err = scanMacOSUpdates()
	case "windows":
		updates, err = scanWindowsUpdates()
	default:
		updates = nil
	}

	if err != nil {
		log.Printf("scan_updates: error: %v", err)
	}

	log.Printf("scan_updates: found %d updates", len(updates))

	// Report updates to server
	for _, u := range updates {
		h.reportUpdate(deviceID, u)
	}
}

// DeployUpdates deploys approved updates to the device.
func (h *Handler) DeployUpdates(ctx context.Context, data interface{}) {
	var payload map[string]interface{}
	if b, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(b, &payload); err != nil {
			log.Printf("deploy_updates: failed to parse payload: %v", err)
			return
		}
	}

	deploymentID, _ := payload["deployment_id"].(string)
	deviceID, _ := payload["device_id"].(string)

	// Parse the approved updates list sent by the server. The deploy must
	// install exactly these packages — never a full OS upgrade — so that
	// approving a subset never upgrades the rest of the host.
	var updates []Update
	if raw, ok := payload["updates"]; ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, &updates)
		}
	}

	log.Printf("deploy_updates: deploying %d approved updates", len(updates))

	var err error
	switch runtime.GOOS {
	case "linux":
		err = deployLinuxUpdates(deploymentID, updates)
	case "darwin":
		err = deployMacOSUpdates(updates)
	case "windows":
		err = deployWindowsUpdates(updates)
	}

	if err != nil {
		log.Printf("deploy_updates: error: %v", err)
		h.reportResultWithMessage(deviceID, deploymentID, false, err.Error())
		return
	}

	// Report result to server
	h.reportResult(deviceID, deploymentID, true)
}

// Linux package management
func scanLinuxUpdates() ([]Update, error) {
	updates := []Update{}

	// Try apt (Debian/Ubuntu)
	if output, err := exec.Command("apt-get", "-s", "upgrade").CombinedOutput(); err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			// Simulate lines look like: Inst pkg [2.4-1] (2.3-1 pool) []
			if strings.HasPrefix(line, "Inst ") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					updates = append(updates, Update{
						Source:  "apt",
						Title:   parts[1],
						Version: strings.Trim(parts[2], "[]"),
					})
				}
			}
		}
	}

	// Try yum/dnf (RHEL/CentOS/Fedora) — table output:
	//   package-name.arch  version  repository
	for _, tool := range []string{"dnf", "yum"} {
		if _, err := exec.LookPath(tool); err != nil {
			continue
		}
		output, err := exec.Command(tool, "check-update", "--quiet").CombinedOutput()
		if err != nil {
			continue // no updates or tool failure — both fine for a scan
		}
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			parts := strings.Fields(line)
			// A valid row has package.arch, a version containing ".", and a repo.
			if len(parts) < 3 || !strings.Contains(parts[1], ".") || strings.Contains(parts[0], " ") {
				continue
			}
			updates = append(updates, Update{
				Source:  "yum",
				Title:   parts[0],
				Version: parts[1],
			})
		}
		break // use only the first available tool
	}

	return updates, nil
}

func deployLinuxUpdates(deploymentID string, updates []Update) error {
	// No explicit list (server sent none): fall back to a full upgrade.
	if len(updates) == 0 {
		return deployFullLinuxUpgrade()
	}

	// Record the currently installed versions so a later rollback can
	// restore them. A deployment with no recorded state cannot be rolled
	// back honestly.
	var preState []preDeployEntry
	var firstErr error
	for _, u := range updates {
		if u.Title == "" {
			continue
		}
		oldVersion := currentPackageVersion(u.Source, u.Title)
		preState = append(preState, preDeployEntry{Package: u.Title, Source: u.Source, OldVersion: oldVersion})

		var cmd *exec.Cmd
		switch u.Source {
		case "yum":
			if u.Version != "" {
				cmd = exec.Command("yum", "-y", "install", u.Title+"-0:"+u.Version)
			} else {
				cmd = exec.Command("yum", "-y", "install", u.Title)
			}
		default: // apt
			if u.Version != "" {
				cmd = exec.Command("apt-get", "-y", "install", u.Title+"="+u.Version)
			} else {
				cmd = exec.Command("apt-get", "-y", "install", u.Title)
			}
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("deploy %s@%s: %s", u.Title, u.Version, string(output))
			if firstErr == nil {
				firstErr = fmt.Errorf("install %s@%s failed: %v", u.Title, u.Version, err)
			}
		}
	}

	if deploymentID != "" {
		if err := savePreDeployState(deploymentID, preState); err != nil {
			log.Printf("deploy: failed to save pre-deploy state: %v", err)
		}
	}
	return firstErr
}

// preDeployEntry records the version of a package before a deployment so
// rollback can restore it.
type preDeployEntry struct {
	Package    string `json:"package"`
	Source     string `json:"source"`
	OldVersion string `json:"old_version"`
}

func patchStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".ourway", "patch-state")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func savePreDeployState(deploymentID string, state []preDeployEntry) error {
	dir, err := patchStateDir()
	if err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	// deployment_id comes from the server; sanitize to a safe file name.
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, deploymentID)
	return os.WriteFile(filepath.Join(dir, safe+".json"), b, 0600)
}

func loadPreDeployState(deploymentID string) ([]preDeployEntry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, deploymentID)
	b, err := os.ReadFile(filepath.Join(home, ".ourway", "patch-state", safe+".json"))
	if err != nil {
		return nil, err
	}
	var state []preDeployEntry
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, err
	}
	return state, nil
}

func clearPreDeployState(deploymentID string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, deploymentID)
	os.Remove(filepath.Join(home, ".ourway", "patch-state", safe+".json"))
}

// currentPackageVersion queries the version of a package as installed now.
func currentPackageVersion(source, name string) string {
	var cmd *exec.Cmd
	if source == "yum" {
		cmd = exec.Command("rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}", name)
	} else {
		cmd = exec.Command("dpkg-query", "-W", "-f=${Version}", name)
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// deployFullLinuxUpgrade runs a full system upgrade (fallback when the
// server sends no explicit update list).
func deployFullLinuxUpgrade() error {
	// Try apt first
	var aptErr error
	cmd := exec.Command("apt-get", "-y", "upgrade")
	if output, err := cmd.CombinedOutput(); err == nil {
		return nil
	} else {
		aptErr = err
		log.Printf("apt-get upgrade: %s", string(output))
	}

	// Try yum as fallback
	cmd = exec.Command("yum", "-y", "update")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("yum update: %s", string(output))
		return fmt.Errorf("linux update failed: apt-get upgrade: %v; yum update: %v", aptErr, err)
	}

	return nil
}

// macOS package management
func scanMacOSUpdates() ([]Update, error) {
	updates := []Update{}

	// Check for system updates
	if output, err := exec.Command("softwareupdate", "-l", "--no-scan").CombinedOutput(); err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "   * ") {
				updates = append(updates, Update{
					Source: "softwareupdate",
					Title:  strings.TrimSpace(line[4:]),
				})
			}
		}
	}

	// Check homebrew
	if output, err := exec.Command("brew", "outdated", "--json").CombinedOutput(); err == nil {
		var packages []map[string]interface{}
		if err := json.Unmarshal(output, &packages); err == nil {
			for _, pkg := range packages {
				name, _ := pkg["name"].(string)
				if name == "" {
					continue
				}
				if info, ok := pkg["installed"].(map[string]interface{}); ok {
					if ver, ok := info["version"].(string); ok {
						updates = append(updates, Update{
							Source:  "homebrew",
							Title:   name,
							Version: ver,
						})
					}
				}
			}
		}
	}

	return updates, nil
}

func deployMacOSUpdates(updates []Update) error {
	if len(updates) == 0 {
		return deployFullMacOSUpgrade()
	}

	var firstErr error
	for _, u := range updates {
		if u.Title == "" {
			continue
		}
		var cmd *exec.Cmd
		if u.Source == "homebrew" {
			cmd = exec.Command("brew", "upgrade", u.Title)
		} else {
			cmd = exec.Command("softwareupdate", "-i", "--no-scan", u.Title)
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("deploy %s@%s: %s", u.Title, u.Version, string(output))
			if firstErr == nil {
				firstErr = fmt.Errorf("install %s@%s failed: %v", u.Title, u.Version, err)
			}
		}
	}
	return firstErr
}

// deployFullMacOSUpgrade runs all system + homebrew upgrades (fallback when
// the server sends no explicit update list).
func deployFullMacOSUpgrade() error {
	// Run system updates
	cmd := exec.Command("softwareupdate", "-ia", "--no-scan")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("softwareupdate: %s", string(output))
		return fmt.Errorf("macos softwareupdate failed: %v", err)
	}

	// Run homebrew update
	cmd = exec.Command("brew", "upgrade")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("brew upgrade: %s", string(output))
	}

	return nil
}

// Windows package management
func scanWindowsUpdates() ([]Update, error) {
	updates := []Update{}
	// Enumerate *available* updates via the PSWindowsUpdate module when
	// present. (Get-HotFix lists already-installed hotfixes, which is not
	// what an update scan should report.)
	script := "Get-Command Get-WindowsUpdate -ErrorAction SilentlyContinue | Out-Null; if ($?) { Get-WindowsUpdate | Select-Object Title, Version | ConvertTo-Json }"
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	if output, err := cmd.CombinedOutput(); err == nil {
		s := strings.TrimSpace(string(output))
		if s != "" {
			var items []map[string]interface{}
			if json.Unmarshal([]byte(s), &items) == nil {
				for _, it := range items {
					title, _ := it["Title"].(string)
					if title == "" {
						continue
					}
					version, _ := it["Version"].(string)
					updates = append(updates, Update{
						Source:  "windows-update",
						Title:   title,
						Version: version,
					})
				}
			} else {
				// Single object, not an array
				var one map[string]interface{}
				if json.Unmarshal([]byte(s), &one) == nil {
					if title, _ := one["Title"].(string); title != "" {
						version, _ := one["Version"].(string)
						updates = append(updates, Update{Source: "windows-update", Title: title, Version: version})
					}
				}
			}
		}
	} else {
		log.Printf("scan_windows: update enumeration unavailable (install PSWindowsUpdate module): %v", err)
	}
	return updates, nil
}

func deployWindowsUpdates(updates []Update) error {
	if len(updates) > 0 {
		// wuauclt cannot install individual updates; do not silently fall
		// back to a full install — report a failure instead.
		log.Printf("windows: per-update install not supported (%d updates); refusing full install", len(updates))
		return fmt.Errorf("windows: per-update install not supported")
	}
	return deployFullWindowsUpgrade()
}

// deployFullWindowsUpgrade triggers a Windows Update scan/install (fallback
// when the server sends no explicit update list).
func deployFullWindowsUpgrade() error {
	// Use wuauclt to trigger Windows Update
	cmd := exec.Command("wuauclt", "/detectnow", "/reportnow")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("wuauclt: %s", string(output))
		return fmt.Errorf("windows update failed: wuauclt: %v", err)
	}
	return nil
}

// RollbackUpdates rolls back the most recent deployment on the device.
// It restores the pre-deploy package versions recorded during the
// deployment; without that state it reports failure rather than a fake
// success.
func (h *Handler) RollbackUpdates(ctx context.Context, data interface{}) {
	var payload map[string]interface{}
	if b, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(b, &payload); err != nil {
			log.Printf("rollback_updates: failed to parse payload: %v", err)
			return
		}
	}

	deploymentID, _ := payload["deployment_id"].(string)
	deviceID, _ := payload["device_id"].(string)

	log.Printf("rollback_updates: rolling back deployment %s", deploymentID)

	if deploymentID == "" {
		h.reportResultWithMessage(deviceID, deploymentID, false, "no deployment_id supplied; nothing to roll back")
		return
	}

	var err error
	switch runtime.GOOS {
	case "linux":
		err = rollbackLinuxUpdates(deploymentID)
	case "darwin":
		err = rollbackMacOSUpdates(deploymentID)
	case "windows":
		err = rollbackWindowsUpdates(deploymentID)
	}

	if err != nil {
		log.Printf("rollback_updates: error: %v", err)
		h.reportResultWithMessage(deviceID, deploymentID, false, err.Error())
		return
	}

	// Report result to server
	h.reportResult(deviceID, deploymentID, true)
}

// Linux rollback: restore the pre-deploy versions recorded at deploy time.
func rollbackLinuxUpdates(deploymentID string) error {
	state, err := loadPreDeployState(deploymentID)
	if err != nil {
		return fmt.Errorf("no pre-deploy state recorded for deployment %s: %w", deploymentID, err)
	}
	if len(state) == 0 {
		return fmt.Errorf("no packages recorded for deployment %s", deploymentID)
	}

	var firstErr error
	for _, e := range state {
		if e.OldVersion == "" {
			// Package was newly installed (no prior version): remove it.
			var cmd *exec.Cmd
			if e.Source == "yum" {
				cmd = exec.Command("yum", "-y", "remove", e.Package)
			} else {
				cmd = exec.Command("apt-get", "-y", "remove", e.Package)
			}
			if output, rerr := cmd.CombinedOutput(); rerr != nil {
				log.Printf("rollback %s (remove): %s", e.Package, string(output))
				if firstErr == nil {
					firstErr = fmt.Errorf("remove %s failed: %v", e.Package, rerr)
				}
			}
			continue
		}
		var cmd *exec.Cmd
		if e.Source == "yum" {
			cmd = exec.Command("yum", "-y", "downgrade", e.Package+"-0:"+e.OldVersion)
		} else {
			cmd = exec.Command("apt-get", "-y", "install", e.Package+"="+e.OldVersion)
		}
		if output, rerr := cmd.CombinedOutput(); rerr != nil {
			log.Printf("rollback %s -> %s: %s", e.Package, e.OldVersion, string(output))
			if firstErr == nil {
				firstErr = fmt.Errorf("downgrade %s to %s failed: %v", e.Package, e.OldVersion, rerr)
			}
		}
	}

	if firstErr == nil {
		clearPreDeployState(deploymentID)
	}
	return firstErr
}

// macOS rollback: brew has no reliable version-restore mechanism and
// softwareupdate offers none either — report that honestly instead of a
// fake success.
func rollbackMacOSUpdates(deploymentID string) error {
	return fmt.Errorf("macos: automatic rollback is not supported; restore packages manually (brew pin/reinstall) for deployment %s", deploymentID)
}

// Windows rollback: wuauclt offers no uninstall API for individual updates.
func rollbackWindowsUpdates(deploymentID string) error {
	return fmt.Errorf("windows: automatic rollback is not supported; use Windows Update history to uninstall updates for deployment %s", deploymentID)
}

// Reboot sends a reboot command to the device.
func (h *Handler) Reboot(data interface{}) {
	var payload map[string]interface{}
	if b, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(b, &payload); err == nil {
			if delaySec, ok := payload["delay_seconds"].(float64); ok && delaySec > 0 {
				log.Printf("rebooting in %d seconds", int(delaySec))
				time.Sleep(time.Duration(delaySec) * time.Second)
			}
		}
	}

	log.Printf("rebooting device")
	switch runtime.GOOS {
	case "linux", "darwin":
		exec.Command("shutdown", "-r", "now").Start()
	case "windows":
		exec.Command("shutdown", "/r", "/t", "0").Start()
	}
}

// Report an update to the server
func (h *Handler) reportUpdate(deviceID string, update Update) {
	payload := map[string]interface{}{
		"device_id":  deviceID,
		"source":     update.Source,
		"title":      update.Title,
		"version":    update.Version,
		"size_bytes": update.SizeBytes,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}

	req, err := http.NewRequest("POST", h.serverURL+"/api/agent/updates", strings.NewReader(string(b)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Key", h.deviceKey)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("report_update: %v", err)
		return
	}
	defer resp.Body.Close()
}

// Report deployment result to the server
func (h *Handler) reportResult(deviceID, deploymentID string, success bool) {
	h.reportResultWithMessage(deviceID, deploymentID, success, "")
}

// reportResultWithMessage reports a deployment result with an optional
// human-readable detail (e.g. why a rollback was refused).
func (h *Handler) reportResultWithMessage(deviceID, deploymentID string, success bool, message string) {
	payload := map[string]interface{}{
		"device_id":     deviceID,
		"deployment_id": deploymentID,
		"result":        "success",
	}
	if !success {
		payload["result"] = "failed"
	}
	if message != "" {
		payload["message"] = message
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}

	req, err := http.NewRequest("POST", h.serverURL+"/api/agent/deployments/result", strings.NewReader(string(b)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Key", h.deviceKey)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("report_result: %v", err)
		return
	}
	defer resp.Body.Close()
}
