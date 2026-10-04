//go:build linux

package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"ourway/agent/config"
)

const serviceName = "ourway-agent"
const unitPath = "/etc/systemd/system/" + serviceName + ".service"

// Install creates a systemd unit and enables the service.
func Install(cfg *config.Config) error {
	unit := fmt.Sprintf(`[Unit]
Description=OurWay Agent
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/ourway-agent --server %s --key %s
Restart=always
RestartSec=5
User=root
Environment=OURWAY_SERVER=%s
Environment=OURWAY_DEVICE_KEY=%s

[Install]
WantedBy=multi-user.target
`, cfg.ServerURL, cfg.DeviceKey, cfg.ServerURL, cfg.DeviceKey)

	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	fmt.Println("Created systemd unit at", unitPath)

	// Reload daemon
	if err := runCommand("systemctl", "daemon-reload"); err != nil {
		fmt.Printf("Warning: systemctl daemon-reload failed: %v\n", err)
	}

	// Enable and start
	if err := runCommand("systemctl", "enable", serviceName); err != nil {
		return fmt.Errorf("enable service: %w", err)
	}
	fmt.Println("Service enabled")

	if err := runCommand("systemctl", "start", serviceName); err != nil {
		fmt.Printf("Warning: could not start service: %v\n", err)
	} else {
		fmt.Println("Service started")
	}

	return nil
}

// Uninstall stops and removes the systemd service and the agent's
// files (including the running binary). It must run as root.
func Uninstall() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("uninstall must run as root (try: sudo ourway-agent --uninstall)")
	}
	runCommand("systemctl", "stop", serviceName)
	runCommand("systemctl", "disable", serviceName)
	os.Remove(unitPath)
	runCommand("systemctl", "daemon-reload")
	fmt.Println("Service removed")

	os.Remove("/usr/local/bin/ourway-uninstall")
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate agent binary: %w", err)
	}
	dir := filepath.Dir(exe)
	removed, err := RemoveInstallFiles(dir, exe)
	if err != nil {
		fmt.Printf("Skipped file removal: %v\n", err)
		return nil
	}
	for _, p := range removed {
		fmt.Println("Removed", p)
	}
	removeSelfAndDir(dir, exe)
	fmt.Println("Removed", exe)
	return nil
}

// IsInstalled checks if the systemd unit exists.
func IsInstalled() (bool, error) {
	_, err := os.Stat(unitPath)
	return err == nil, nil
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var _ = runtime.GOOS
