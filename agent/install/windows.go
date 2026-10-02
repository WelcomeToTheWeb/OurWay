//go:build windows

package install

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"ourway/agent/config"
)

const ServiceName = "OurWayAgent"

// Install creates a Windows service using sc.exe.
func Install(cfg *config.Config) error {
	// Determine executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Create the service
	if err := runCommand("sc.exe", "create", ServiceName,
		"binPath=", fmt.Sprintf(`"%s" --server %s --key %s`, exe, cfg.ServerURL, cfg.DeviceKey),
		"start=", "auto",
		"display=", "OurWay Agent"); err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	fmt.Println("Service created")

	// Automatic recovery: restart on the 1st-3rd failure (5s/10s/30s)
	// and reset the failure counter after 1 day of stable uptime, so a
	// crashed agent comes back without manual intervention.
	if err := runCommand("sc.exe", "failure", ServiceName,
		"reset=86400",
		"actions=restart/5000/restart/10000/restart/30000"); err != nil {
		fmt.Printf("Warning: could not set service recovery actions: %v\n", err)
	}

	// Start the service
	if err := runCommand("sc.exe", "start", ServiceName); err != nil {
		fmt.Printf("Warning: could not start service: %v\n", err)
	} else {
		fmt.Println("Service started")
	}

	return nil
}

// Uninstall removes the Windows service.
func Uninstall() error {
	runCommand("sc.exe", "stop", ServiceName)
	runCommand("sc.exe", "delete", ServiceName)
	fmt.Println("Service uninstalled")
	return nil
}

// IsInstalled checks if the Windows service exists.
func IsInstalled() (bool, error) {
	output, err := exec.Command("sc.exe", "query", ServiceName).Output()
	if err != nil {
		return false, nil
	}
	return len(output) > 0, nil
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var _ = runtime.GOOS
