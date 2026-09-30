package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Version info - set via ldflags at build time
var (
	Version   = "0.1.0"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// CLI flags
var (
	flagServer = flag.String("server", "http://localhost:8080", "OurWay server URL")
	flagConfig = flag.String("config", "", "Path to config file")
)

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	command := args[0]
	switch command {
	case "register":
		cmdRegister()
	case "install":
		cmdInstall()
	case "uninstall":
		cmdUninstall()
	case "status":
		cmdStatus()
	case "logs":
		cmdLogs()
	case "version":
		cmdVersion()
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command '%s'\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("OurWay CLI - Manage OurWay agent installation and device registration")
	fmt.Println()
	fmt.Printf("Usage: %s [flags] <command>\n\n", os.Args[0])
	fmt.Println("Commands:")
	fmt.Println("  register   Register a new device with the OurWay server")
	fmt.Println("  install    Install the OurWay agent on this machine")
	fmt.Println("  uninstall  Remove the OurWay agent from this machine")
	fmt.Println("  status     Show the status of the OurWay agent")
	fmt.Println("  logs       Show OurWay agent logs")
	fmt.Println("  version    Show CLI version information")
	fmt.Println("  help       Show this help message")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --server   OurWay server URL (default http://localhost:8080)")
	fmt.Println("  --config   Path to config file")
}

// getSystemInfo returns hostname, OS, and arch
func getSystemInfo() (hostname, osName, arch string) {
	hostname, _ = os.Hostname()
	return hostname, runtime.GOOS, runtime.GOARCH
}

// getPublicIP attempts to determine the public IP address
func getPublicIP() string {
	urls := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
	}
	for _, url := range urls {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

// getPrivateIP determines the private IP address
func getPrivateIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// cmdRegister registers this device with the OurWay server
func cmdRegister() {
	hostname, osName, arch := getSystemInfo()
	publicIP := getPublicIP()
	privateIP := getPrivateIP()

	payload := map[string]string{
		"name":          hostname,
		"hostname":      hostname,
		"os":            osName,
		"arch":          arch,
		"agent_version": Version,
	}
	if publicIP != "" {
		payload["public_ip"] = publicIP
	}
	if privateIP != "" {
		payload["private_ip"] = privateIP
	}

	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to marshal request: %v\n", err)
		os.Exit(1)
	}

	url := strings.TrimRight(*flagServer, "/") + "/api/agent/register"
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to connect to server at %s: %v\n", url, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to read response: %v\n", err)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: server returned %d: %s\n", resp.StatusCode, string(respBody))
		os.Exit(1)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to parse response: %v\n", err)
		os.Exit(1)
	}

	deviceKey, _ := result["device_key"].(string)
	fmt.Println("Device registered successfully!")
	fmt.Printf("Device name: %s\n", hostname)
	fmt.Printf("OS: %s/%s\n", osName, arch)
	if publicIP != "" {
		fmt.Printf("Public IP: %s\n", publicIP)
	}
	if privateIP != "" {
		fmt.Printf("Private IP: %s\n", privateIP)
	}
	if deviceKey != "" {
		fmt.Printf("\nDevice Key: %s\n", deviceKey)
		fmt.Println("Save this key - it will be used for authentication.")
	}
}

// cmdInstall installs the OurWay agent
func cmdInstall() {
	hostname, osName, arch := getSystemInfo()

	fmt.Printf("Installing OurWay agent on %s (%s/%s)...\n", hostname, osName, arch)

	// Determine install directory
	var installDir, binPath string
	if osName == "windows" {
		installDir = `C:\Program Files\OurWay\Agent`
		binPath = filepath.Join(installDir, "ourway-agent.exe")
	} else {
		installDir = "/opt/ourway/agent"
		binPath = filepath.Join(installDir, "ourway-agent")
	}

	// Create install directory
	if err := os.MkdirAll(installDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create install directory: %v\n", err)
		os.Exit(1)
	}

	// Check if binary already exists locally, otherwise download
	if !fileExists(binPath) {
		// Try to find binary in the same directory as the CLI first
		if exePath, err := os.Executable(); err == nil {
			localBin := filepath.Join(filepath.Dir(exePath), "ourway-agent")
			if runtime.GOOS == "windows" {
				localBin = localBin + ".exe"
			}
			if fileExists(localBin) {
				fmt.Println("Using local agent binary...")
				data, err := os.ReadFile(localBin)
				if err == nil {
					if err := os.WriteFile(binPath, data, 0755); err == nil {
						binPath = filepath.Join(filepath.Dir(exePath), "ourway-agent")
						if runtime.GOOS == "windows" {
							binPath = binPath + ".exe"
						}
					}
				}
			}
		}

		// If still not found, try to download from server
		if !fileExists(binPath) {
			fmt.Println("Downloading agent binary from server...")
			url := strings.TrimRight(*flagServer, "/") + "/api/agent/binary?os=" + osName + "&arch=" + arch
			resp, err := http.Get(url)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to download binary: %v\n", err)
				os.Exit(1)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				fmt.Fprintf(os.Stderr, "Error: server returned %d: %s\n", resp.StatusCode, string(body))
				fmt.Println("Hint: You can copy the agent binary to the same directory as this CLI.")
				os.Exit(1)
			}

			out, err := os.Create(binPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to create binary file: %v\n", err)
				os.Exit(1)
			}
			defer out.Close()

			if _, err := io.Copy(out, resp.Body); err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to write binary: %v\n", err)
				os.Exit(1)
			}
			if err := os.Chmod(binPath, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to set permissions: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Agent binary downloaded successfully.")
		}
	}

	// Create config directory and config file
	configDir := filepath.Join(installDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create config directory: %v\n", err)
		os.Exit(1)
	}

	// Create or update device config
	deviceConfig := map[string]interface{}{
		"server_url":     *flagServer,
		"device_name":    hostname,
		"collect_interval": 30,
		"log_level":      "info",
	}
	configData, _ := json.MarshalIndent(deviceConfig, "", "  ")
	if err := os.WriteFile(filepath.Join(configDir, "device.json"), configData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to write config: %v\n", err)
		os.Exit(1)
	}

	// Create log directory
	logDir := filepath.Join(installDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create log directory: %v\n", err)
		os.Exit(1)
	}

	// Create service file based on OS
	if err := createService(osName, binPath, configDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create service: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("OurWay agent installed successfully!")
	fmt.Printf("Binary: %s\n", binPath)
	fmt.Printf("Config: %s\n", configDir)
	fmt.Printf("Logs:   %s\n", logDir)
	fmt.Println()
	fmt.Println("Run 'ourway-cli status' to check agent status.")
	fmt.Println("Run 'ourway-cli logs' to view agent logs.")
}

// createService creates a systemd or launchd service for the agent
func createService(osName, binPath, configDir string) error {
	switch osName {
	case "linux":
		// Create systemd service
		unitContent := fmt.Sprintf(`[Unit]
Description=OurWay RMM Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=%s --config %s/device.json
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, binPath, configDir)

		unitPath := "/etc/systemd/system/ourway-agent.service"
		if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
			return fmt.Errorf("failed to write systemd unit: %w", err)
		}

		// Reload systemd and enable service
		runCmd("systemctl", "daemon-reload")
		runCmd("systemctl", "enable", "ourway-agent")
		runCmd("systemctl", "start", "ourway-agent")

	case "darwin":
		// Create launchd plist
		plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.ourway.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--config</string>
        <string>%s/device.json</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/ourway-agent.out.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/ourway-agent.err.log</string>
</dict>
</plist>
`, binPath, configDir)

		plistPath := "/Library/LaunchDaemons/com.ourway.agent.plist"
		if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
			return fmt.Errorf("failed to write launchd plist: %w", err)
		}

		runCmd("launchctl", "load", "-w", plistPath)
	case "windows":
		// Create Windows service using sc.exe
		runCmd("sc", "create", "OurWayAgent",
			"binPath=", binPath+" --config "+configDir+"\\device.json",
			"start=auto")
		runCmd("sc", "start", "OurWayAgent")
	}
	return nil
}

// cmdUninstall removes the OurWay agent
func cmdUninstall() {
	hostname, osName, _ := getSystemInfo()

	fmt.Printf("Uninstalling OurWay agent from %s (%s)...\n", hostname, osName)

	switch osName {
	case "linux":
		runCmd("systemctl", "stop", "ourway-agent")
		runCmd("systemctl", "disable", "ourway-agent")
		runCmd("rm", "-f", "/etc/systemd/system/ourway-agent.service")
		runCmd("systemctl", "daemon-reload")
		runCmd("rm", "-rf", "/opt/ourway")
	case "darwin":
		runCmd("launchctl", "unload", "-w", "/Library/LaunchDaemons/com.ourway.agent.plist")
		runCmd("rm", "-f", "/Library/LaunchDaemons/com.ourway.agent.plist")
		runCmd("rm", "-rf", "/opt/ourway")
	case "windows":
		runCmd("sc", "stop", "OurWayAgent")
		runCmd("sc", "delete", "OurWayAgent")
		runCmd("rmdir", "/S", "/Q", `C:\Program Files\OurWay`)
	}

	fmt.Println("OurWay agent uninstalled successfully.")
}

// cmdStatus checks if the agent is running
func cmdStatus() {
	hostname, osName, _ := getSystemInfo()
	fmt.Printf("Checking OurWay agent status on %s (%s)...\n", hostname, osName)

	switch osName {
	case "linux":
		cmd := exec.Command("systemctl", "is-active", "ourway-agent")
		output, err := cmd.Output()
		status := strings.TrimSpace(string(output))
		if err != nil {
			status = "inactive"
		}
		if status == "active" {
			fmt.Println("Agent: RUNNING")
			// Show more details
			detailCmd := exec.Command("systemctl", "status", "ourway-agent", "--no-pager", "-l")
			detailOut, _ := detailCmd.CombinedOutput()
			fmt.Println()
			fmt.Println(string(detailOut))
		} else {
			fmt.Println("Agent: STOPPED")
			fmt.Println("Start with: systemctl start ourway-agent")
		}
	case "darwin":
		cmd := exec.Command("launchctl", "list", "com.ourway.agent")
		output, err := cmd.Output()
		if err != nil {
			fmt.Println("Agent: STOPPED")
			fmt.Println("Start with: launchctl load -w /Library/LaunchDaemons/com.ourway.agent.plist")
			return
		}
		fmt.Println("Agent: RUNNING")
		fmt.Println(strings.TrimSpace(string(output)))
	case "windows":
		cmd := exec.Command("sc", "query", "OurWayAgent")
		output, err := cmd.Output()
		if err != nil {
			fmt.Println("Agent: NOT INSTALLED or STOPPED")
			return
		}
		fmt.Println(string(output))
	}
}

// cmdLogs shows agent logs
func cmdLogs() {
	hostname, osName, _ := getSystemInfo()
	fmt.Printf("Showing OurWay agent logs on %s (%s):\n", hostname, osName)
	fmt.Println()

	switch osName {
	case "linux":
		cmd := exec.Command("journalctl", "-u", "ourway-agent", "-f", "--no-pager", "-n", "50")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "darwin":
		// Show recent lines from log file
		file, err := os.Open("/tmp/ourway-agent.out.log")
		if err != nil {
			fmt.Println("No log file found at /tmp/ourway-agent.out.log")
			return
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	case "windows":
		cmd := exec.Command("powershell", "-Command",
			"Get-WinEvent -LogName Application -Source OurWayAgent -MaxEvents 50 -ErrorAction SilentlyContinue")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
}

// cmdVersion prints version information
func cmdVersion() {
	fmt.Println("OurWay CLI")
	fmt.Printf("Version:   %s\n", Version)
	fmt.Printf("Build:     %s\n", BuildTime)
	fmt.Printf("Commit:    %s\n", GitCommit)
	fmt.Printf("Go:        %s\n", runtime.Version())
	fmt.Printf("OS/Arch:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runCmd runs a command and returns nil on success
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
