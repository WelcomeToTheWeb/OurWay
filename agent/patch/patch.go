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
	// ExternalID is the source's own identifier (Windows Update ID).
	ExternalID string `json:"external_id,omitempty"`
	KB         string `json:"kb,omitempty"`
	Severity   string `json:"severity,omitempty"`
	Category   string `json:"category,omitempty"`
}

// Handler manages patch operations for the agent.
type Handler struct {
	deviceKey  string
	deviceID   string
	serverURL  string
	httpClient *http.Client
}

// NewHandler creates a new patch handler. deviceID is this agent's own
// device ID (resolved at startup); server payloads addressed to a
// different device are refused. An empty deviceID disables the check with
// a warning — the WebSocket connection is already key-authenticated, so
// this is defense in depth, not the primary trust boundary.
func NewHandler(deviceKey, serverURL, deviceID string) *Handler {
	return &Handler{
		deviceKey: deviceKey,
		deviceID:  deviceID,
		serverURL: strings.TrimSuffix(serverURL, "/ws"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// verifyDeviceID checks that a server payload is addressed to this device.
// It fails open (with a warning) when this agent's own ID is unknown,
// since the connection itself is already key-authenticated.
func (h *Handler) verifyDeviceID(op, payloadDeviceID string) bool {
	if h.deviceID == "" {
		log.Printf("%s: warning: own device ID unknown; skipping device_id verification", op)
		return true
	}
	if payloadDeviceID != h.deviceID {
		return false
	}
	return true
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

	if !h.verifyDeviceID("scan_updates", deviceID) {
		log.Printf("scan_updates: payload device_id %q does not match this device (%q); ignoring", deviceID, h.deviceID)
		return
	}

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

	// Refuse payloads addressed to a different device. The report is sent
	// under THIS device's ID (the server validates report device_id
	// against the key owner), so the deployment's failure is recorded.
	if !h.verifyDeviceID("deploy_updates", deviceID) {
		log.Printf("deploy_updates: payload device_id %q does not match this device (%q); refusing", deviceID, h.deviceID)
		h.reportResultWithMessage(h.deviceID, deploymentID, "deploy", false, "deploy payload addressed to a different device; refused")
		return
	}

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
	var reboot bool
	switch runtime.GOOS {
	case "linux":
		err = deployLinuxUpdates(deploymentID, updates)
		if err == nil {
			_, serr := os.Stat("/var/run/reboot-required")
			reboot = serr == nil
		}
	case "darwin":
		err = deployMacOSUpdates(updates)
	case "windows":
		reboot, err = deployWindowsUpdates(updates)
	}

	if err != nil {
		log.Printf("deploy_updates: error: %v", err)
		h.reportResultWithMessage(deviceID, deploymentID, "deploy", false, err.Error())
		return
	}

	// Report result to server
	h.reportDeploy(deviceID, deploymentID, reboot)
}

// Linux package management
func scanLinuxUpdates() ([]Update, error) {
	updates := []Update{}

	// Try apt (Debian/Ubuntu). Refresh the package lists first (best
	// effort) so the simulation sees what is actually available; a stale
	// cache reports nothing, forever.
	if _, err := exec.LookPath("apt-get"); err == nil {
		_ = aptCmd("update", "-qq").Run()
		if output, err := aptCmd("-s", "upgrade").CombinedOutput(); err == nil {
			updates = append(updates, parseAptSimulate(string(output))...)
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

// aptCmd builds a non-interactive apt-get command: without
// DEBIAN_FRONTEND a package's debconf or conffile prompt blocks the
// deploy forever on a service with no terminal.
func aptCmd(args ...string) *exec.Cmd {
	full := append([]string{"-o", "Dpkg::Options::=--force-confold", "-o", "Dpkg::Options::=--force-confdef"}, args...)
	cmd := exec.Command("apt-get", full...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	return cmd
}

// parseAptSimulate extracts upgrades from `apt-get -s upgrade` output.
// Lines look like:
//
//	Inst pkg [1.0-1] (1.1-1 Debian:12/stable [amd64])
//
// where the bracketed value is the INSTALLED version and the
// parenthesised one the candidate — the candidate is what must be
// deployed. A new dependency has no bracketed part.
func parseAptSimulate(output string) []Update {
	var updates []Update
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		cand := parts[2]
		if strings.HasPrefix(cand, "[") && len(parts) >= 4 {
			cand = parts[3]
		}
		if !strings.HasPrefix(cand, "(") {
			continue
		}
		updates = append(updates, Update{
			Source:  "apt",
			Title:   parts[1],
			Version: strings.TrimPrefix(cand, "("),
		})
	}
	return updates
}

// rpmInstallSpec builds a NEVRA ("name-epoch:version-release.arch") for
// yum/dnf from a check-update row ("name.arch", "version-release"). dnf
// already includes the epoch ("1:2.3-1"); without one, epoch 0 is assumed.
func rpmInstallSpec(nameArch, version string) string {
	if version == "" {
		return nameArch
	}
	name, arch := nameArch, ""
	if i := strings.LastIndex(nameArch, "."); i > 0 {
		name, arch = nameArch[:i], nameArch[i:]
	}
	if !strings.Contains(version, ":") {
		version = "0:" + version
	}
	return name + "-" + version + arch
}

// rpmTool returns dnf when present, else yum.
func rpmTool() string {
	if _, err := exec.LookPath("dnf"); err == nil {
		return "dnf"
	}
	return "yum"
}

func deployLinuxUpdates(deploymentID string, updates []Update) error {
	// An empty list is never a request for a full upgrade: it means the
	// server sent nothing to install, and running `apt-get upgrade` /
	// `yum update` here would upgrade the whole OS unrequested.
	if len(updates) == 0 {
		return fmt.Errorf("no updates provided; refusing to run a full OS upgrade")
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
			cmd = exec.Command(rpmTool(), "-y", "install", rpmInstallSpec(u.Title, u.Version))
		default: // apt
			target := u.Title
			if u.Version != "" {
				target += "=" + u.Version
			}
			cmd = aptCmd("-y", "install", target)
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

// patchStateDir returns (creating it) the directory holding pre-deploy
// state. HOME can be unset for a service, so fall back to a system path.
func patchStateDir() (string, error) {
	dir := "/var/lib/ourway/patch-state"
	if home, err := os.UserHomeDir(); err == nil {
		dir = filepath.Join(home, ".ourway", "patch-state")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// stateFile maps a deployment ID to its state file. The ID comes from
// the server, so it is sanitised to a safe file name.
func stateFile(deploymentID string) (string, error) {
	dir, err := patchStateDir()
	if err != nil {
		return "", err
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, deploymentID)
	return filepath.Join(dir, safe+".json"), nil
}

func savePreDeployState(deploymentID string, state []preDeployEntry) error {
	path, err := stateFile(deploymentID)
	if err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

func loadPreDeployState(deploymentID string) ([]preDeployEntry, error) {
	path, err := stateFile(deploymentID)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
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
	if path, err := stateFile(deploymentID); err == nil {
		os.Remove(path)
	}
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
	// An empty list is never a request for a full upgrade (see
	// deployLinuxUpdates).
	if len(updates) == 0 {
		return fmt.Errorf("no updates provided; refusing to run a full OS upgrade")
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

// Windows package management (see winupdate.go).

func scanWindowsUpdates() ([]Update, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", wuScanScript).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("windows update scan: %w: %s", err, truncate(strings.TrimSpace(string(out)), 300))
	}
	return parseWindowsScan(string(out))
}

// deployWindowsUpdates installs exactly the approved updates (by Windows
// Update ID) and reports whether a reboot is required. An empty list is
// never a request for a full upgrade.
func deployWindowsUpdates(updates []Update) (reboot bool, err error) {
	if len(updates) == 0 {
		return false, fmt.Errorf("no updates provided; refusing to run a full OS upgrade")
	}
	ids, err := windowsUpdateIDs(updates)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", wuInstallScript)
	cmd.Env = append(os.Environ(), "OURWAY_UPDATE_IDS="+strings.Join(ids, ","))
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return false, fmt.Errorf("windows update install: %w: %s", runErr, truncate(strings.TrimSpace(string(out)), 300))
	}
	return parseWindowsInstall(string(out))
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

	// Refuse payloads addressed to a different device (see DeployUpdates).
	if !h.verifyDeviceID("rollback_updates", deviceID) {
		log.Printf("rollback_updates: payload device_id %q does not match this device (%q); refusing", deviceID, h.deviceID)
		h.reportResultWithMessage(h.deviceID, deploymentID, "rollback", false, "rollback payload addressed to a different device; refused")
		return
	}

	log.Printf("rollback_updates: rolling back deployment %s", deploymentID)

	if deploymentID == "" {
		h.reportResultWithMessage(deviceID, deploymentID, "rollback", false, "no deployment_id supplied; nothing to roll back")
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
		h.reportResultWithMessage(deviceID, deploymentID, "rollback", false, err.Error())
		return
	}

	// Report result to server
	h.reportResultWithMessage(deviceID, deploymentID, "rollback", true, "")
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
				cmd = exec.Command(rpmTool(), "-y", "remove", e.Package)
			} else {
				cmd = aptCmd("-y", "remove", e.Package)
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
			cmd = exec.Command(rpmTool(), "-y", "downgrade", e.Package+"-"+e.OldVersion)
		} else {
			cmd = aptCmd("-y", "--allow-downgrades", "install", e.Package+"="+e.OldVersion)
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
		"device_id":   deviceID,
		"source":      update.Source,
		"title":       update.Title,
		"version":     update.Version,
		"size_bytes":  update.SizeBytes,
		"external_id": update.ExternalID,
		"kb":          update.KB,
		"severity":    update.Severity,
		"category":    update.Category,
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
	h.reportResultWithMessage(deviceID, deploymentID, "deploy", success, "")
}

// reportResultWithMessage reports a deployment or rollback result with an
// optional human-readable detail (e.g. why a rollback was refused). kind
// is "deploy" or "rollback"; the server records it on the per-device
// result row.
func (h *Handler) reportResultWithMessage(deviceID, deploymentID string, kind string, success bool, message string) {
	h.postResult(deviceID, deploymentID, kind, success, message, false)
}

// reportDeploy reports a successful deploy, flagging a required reboot.
func (h *Handler) reportDeploy(deviceID, deploymentID string, rebootRequired bool) {
	h.postResult(deviceID, deploymentID, "deploy", true, "", rebootRequired)
}

func (h *Handler) postResult(deviceID, deploymentID, kind string, success bool, message string, rebootRequired bool) {
	payload := map[string]interface{}{
		"device_id":     deviceID,
		"deployment_id": deploymentID,
		"result":        "success",
		"kind":          kind,
	}
	if !success {
		payload["result"] = "failed"
	}
	if message != "" {
		payload["message"] = message
	}
	if rebootRequired {
		payload["reboot_required"] = true
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
