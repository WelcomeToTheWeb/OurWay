//go:build darwin

package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"ourway/agent/config"
)

const label = "com.ourway.agent"

// Install creates a launchd plist and loads it.
func Install(cfg *config.Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	dir := filepath.Dir(plistPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/ourway-agent</string>
        <string>--server</string>
        <string>%s</string>
        <string>--key</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/Library/Logs/ourway-agent.log</string>
    <key>StandardErrorPath</key>
    <string>%s/Library/Logs/ourway-agent.log</string>
</dict>
</plist>
`, label, cfg.ServerURL, cfg.DeviceKey, home, home)

	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	fmt.Println("Created launchd plist at", plistPath)

	// Load the agent
	if err := runCommand("launchctl", "load", plistPath); err != nil {
		fmt.Printf("Warning: launchctl load failed: %v\n", err)
	} else {
		fmt.Println("Agent loaded")
	}

	return nil
}

// Uninstall removes the launchd job (the per-user agent this package
// installs and the system daemon the installer creates) and the
// agent's files, including the running binary.
func Uninstall() error {
	home, _ := os.UserHomeDir()
	plists := []string{
		filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
		"/Library/LaunchDaemons/" + label + ".plist",
	}
	for _, p := range plists {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		runCommand("launchctl", "unload", "-w", p)
		if err := os.Remove(p); err != nil {
			fmt.Printf("Warning: could not remove %s (run with sudo?): %v\n", p, err)
		} else {
			fmt.Println("Removed", p)
		}
	}
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

// IsInstalled checks if the launchd plist exists.
func IsInstalled() (bool, error) {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	_, err := os.Stat(plistPath)
	return err == nil, nil
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var _ = runtime.GOOS
