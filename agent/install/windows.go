//go:build windows

package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

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

	if err := RegisterUninstallEntry(exe); err != nil {
		fmt.Printf("Warning: could not add the Add/Remove Programs entry: %v\n", err)
	}

	// Start the service
	if err := runCommand("sc.exe", "start", ServiceName); err != nil {
		fmt.Printf("Warning: could not start service: %v\n", err)
	} else {
		fmt.Println("Service started")
	}

	return nil
}

// arpKey is the Add/Remove Programs entry ("Apps & features").
const arpKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\OurWayAgent`

// RegisterUninstallEntry lists the agent in Add/Remove Programs with an
// uninstall command that runs this binary's --uninstall.
func RegisterUninstallEntry(exe string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, arpKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for name, val := range map[string]string{
		"DisplayName":     "OurWay Agent",
		"DisplayVersion":  config.Version,
		"Publisher":       "OurWay",
		"InstallLocation": filepath.Dir(exe),
		"UninstallString": fmt.Sprintf(`"%s" --uninstall --pause`, exe),
	} {
		if err := k.SetStringValue(name, val); err != nil {
			return err
		}
	}
	k.SetDWordValue("NoModify", 1)
	k.SetDWordValue("NoRepair", 1)
	return nil
}

func isElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// relaunchElevated reruns this binary with --uninstall through the UAC
// prompt ("runas"); the elevated copy does the work.
func relaunchElevated(exe string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	args, _ := windows.UTF16PtrFromString("--uninstall --yes --pause")
	if err := windows.ShellExecute(0, verb, file, args, nil, windows.SW_NORMAL); err != nil {
		return fmt.Errorf("could not request administrator rights: %w", err)
	}
	return ErrElevating
}

// Uninstall removes the service, running remote-control processes, the
// Add/Remove Programs entry, logs and the install directory. The
// running binary cannot delete itself, so a detached helper removes it
// (and the directory) just after this process exits.
func Uninstall() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate agent binary: %w", err)
	}
	if !isElevated() {
		return relaunchElevated(exe)
	}

	runCommand("sc.exe", "stop", ServiceName)
	runCommand("sc.exe", "delete", ServiceName)
	fmt.Println("Service removed")
	runCommand("taskkill.exe", "/F", "/IM", "ourway-remote.exe")
	registry.DeleteKey(registry.LOCAL_MACHINE, arpKey)

	if pd := os.Getenv("ProgramData"); pd != "" {
		os.RemoveAll(filepath.Join(pd, "OurWay")) // agent.log, crash logs
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
	// Give the service a moment to release the binary, then delete it,
	// the install directory and the (now possibly empty) vendor folder.
	script := fmt.Sprintf(`ping -n 4 127.0.0.1 >nul & del /f /q "%s" & rmdir "%s" & rmdir "%s"`, exe, dir, filepath.Dir(dir))
	cmd := exec.Command("cmd.exe", "/c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008}
	if err := cmd.Start(); err != nil {
		fmt.Printf("Warning: could not schedule removal of %s: %v\n", exe, err)
	}
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
