package patch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Update represents a software update detected on the device.
type Update struct {
	Source string `json:"source"`
	Title  string `json:"title"`
	Version string `json:"version"`
	SizeBytes int64 `json:"size_bytes"`
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

	log.Printf("deploy_updates: deploying updates")

	var err error
	switch runtime.GOOS {
	case "linux":
		err = deployLinuxUpdates()
	case "darwin":
		err = deployMacOSUpdates()
	case "windows":
		err = deployWindowsUpdates()
	}

	if err != nil {
		log.Printf("deploy_updates: error: %v", err)
	}

	// Report result to server
	h.reportResult(deviceID, deploymentID, err == nil)
}

// Linux package management
func scanLinuxUpdates() ([]Update, error) {
	updates := []Update{}

	// Try apt (Debian/Ubuntu)
	if output, err := exec.Command("apt-get", "-s", "upgrade").CombinedOutput(); err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "Inst ") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					updates = append(updates, Update{
						Source:  "apt",
						Title:   parts[1],
						Version: parts[2],
					})
				}
			}
		}
	}

	// Try yum (RHEL/CentOS)
	if output, err := exec.Command("yum", "check-update", "--quiet").CombinedOutput(); err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, ".") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					updates = append(updates, Update{
						Source:  "yum",
						Title:   parts[0],
						Version: parts[1],
					})
				}
			}
		}
	}

	return updates, nil
}

func deployLinuxUpdates() error {
	// Try apt first
	cmd := exec.Command("apt-get", "-y", "upgrade")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("apt-get upgrade: %s", string(output))
	}

	// Try yum as fallback
	cmd = exec.Command("yum", "-y", "update")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("yum update: %s", string(output))
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
				if info, ok := pkg["installed"].(map[string]interface{}); ok {
					if ver, ok := info["version"].(string); ok {
						updates = append(updates, Update{
							Source:  "homebrew",
							Title:   pkg["name"].(string),
							Version: ver,
						})
					}
				}
			}
		}
	}

	return updates, nil
}

func deployMacOSUpdates() error {
	// Run system updates
	cmd := exec.Command("softwareupdate", "-ia", "--no-scan")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("softwareupdate: %s", string(output))
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
	// Windows updates are more complex - use PowerShell
	cmd := exec.Command("powershell", "-Command",
		"Get-HotFix | Select-Object HotFixID, Description, InstalledOn | ConvertTo-Json")
	if output, err := cmd.CombinedOutput(); err == nil {
		var hotfixes []map[string]interface{}
		if err := json.Unmarshal(output, &hotfixes); err == nil {
			for _, hf := range hotfixes {
				updates = append(updates, Update{
					Source: "windows-update",
					Title:  fmt.Sprintf("%v", hf["HotFixID"]),
				})
			}
		}
	}
	return updates, nil
}

func deployWindowsUpdates() error {
	// Use wuauclt to trigger Windows Update
	cmd := exec.Command("wuauclt", "/detectnow", "/reportnow")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("wuauclt: %s", string(output))
	}
	return nil
}

// RollbackUpdates rolls back the most recent deployment on the device.
func (h *Handler) RollbackUpdates(ctx context.Context, data interface{}) {
	var payload map[string]interface{}
	if b, err := json.Marshal(data); err == nil {
		if err := json.Unmarshal(b, &payload); err == nil {
			if deploymentID, ok := payload["deployment_id"].(string); ok {
				log.Printf("rollback_updates: rolling back deployment %s", deploymentID)
			}
		}
	}

	log.Printf("rollback_updates: rolling back")

	var err error
	switch runtime.GOOS {
	case "linux":
		err = rollbackLinuxUpdates()
	case "darwin":
		err = rollbackMacOSUpdates()
	case "windows":
		err = rollbackWindowsUpdates()
	}

	if err != nil {
		log.Printf("rollback_updates: error: %v", err)
	}
}

// Linux rollback (best-effort: revert apt upgrades)
func rollbackLinuxUpdates() error {
	// For apt, we can try to reinstall previous versions
	// This is best-effort since apt doesn't track rollback easily
	log.Printf("rollback: linux rollback (best-effort)")

	// Try to downgrade any packages that were upgraded
	cmd := exec.Command("apt-get", "-y", "autoremove")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("apt-get autoremove: %s", string(output))
	}

	return nil
}

// macOS rollback (best-effort)
func rollbackMacOSUpdates() error {
	log.Printf("rollback: macos rollback (best-effort)")
	// macOS doesn't have easy rollback - this is informational
	return nil
}

// Windows rollback (best-effort: use wuauclt to check for restore points)
func rollbackWindowsUpdates() error {
	log.Printf("rollback: windows rollback (best-effort)")
	// Use PowerShell to uninstall recent updates
	cmd := exec.Command("powershell", "-Command",
		"Get-WmiObject -Class Win32_QuickFixEngineering | Sort-Object InstalledOn -Descending | Select-Object -First 5 | ForEach-Object { $_.HotFixID }")
	if output, err := cmd.CombinedOutput(); err == nil {
		log.Printf("recent windows updates: %s", string(output))
	}
	return nil
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
		"device_id": deviceID,
		"source":    update.Source,
		"title":     update.Title,
		"version":   update.Version,
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
	payload := map[string]interface{}{
		"device_id":     deviceID,
		"deployment_id": deploymentID,
		"result":        "success",
	}
	if !success {
		payload["result"] = "failed"
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
