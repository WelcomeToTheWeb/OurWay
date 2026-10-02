package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	version   = "1.0.0"
	buildTime = "unknown"
	gitCommit = "unknown"
)

// embeddedAgent holds the agent binary embedded in the installer so the
// installer is fully self-contained. docker/build-agents.sh overwrites
// assets/agent with the matching agent binary before building each
// platform's installer. The checked-in placeholder is a few bytes; a real
// embedded binary is >1KB.
//
//go:embed assets/agent
var embeddedAgent []byte

var (
	flagServer      = flag.String("server", "http://localhost:8080", "OurWay server URL")
	flagKey         = flag.String("key", "", "Device key (use --register to auto-generate)")
	flagRegister    = flag.Bool("register", false, "Register device with server and get key")
	flagInstallDir  = flag.String("install-dir", "", "Installation directory")
	flagSkipService = flag.Bool("skip-service", false, "Don't install as a service")
	flagVersion     = flag.String("version", "", "Agent version to install")
	flagHelp        = flag.Bool("help", false, "Show help")
)

func main() {
	flag.Parse()

	if *flagHelp {
		printHelp()
		os.Exit(0)
	}

	// Determine OS/Arch
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// Determine install directory
	installDir := *flagInstallDir
	if installDir == "" {
		if osName == "windows" {
			installDir = `C:\Program Files\OurWay\Agent`
		} else {
			installDir = "/opt/ourway"
		}
	}

	binName := "ourway-agent"
	if osName == "windows" {
		binName += ".exe"
	}

	fmt.Println("======================================")
	fmt.Println("  OurWay RMM Agent Installer")
	fmt.Println("======================================")
	fmt.Println()
	fmt.Printf("OS: %s/%s\n", osName, arch)
	fmt.Printf("Server: %s\n", *flagServer)
	if *flagKey != "" {
		fmt.Printf("Device Key: %s\n", *flagKey)
	}
	if *flagRegister {
		fmt.Println("Auto-register: yes")
	}
	fmt.Println()

	// Auto-register if needed
	deviceKey := *flagKey
	if deviceKey == "" && *flagRegister {
		deviceKey = registerDevice(osName, arch)
		if deviceKey == "" {
			fmt.Println("Warning: Failed to register device. Continuing without key.")
		}
	}

	// Download/install binary
	fmt.Println("Installing agent binary...")
	binaryPath := installBinary(osName, arch, installDir, binName)
	if binaryPath == "" {
		fmt.Println("Error: Failed to install binary")
		pauseIfWindows()
		os.Exit(1)
	}
	fmt.Printf("Binary: %s\n", binaryPath)

	// Create directories
	os.MkdirAll(filepath.Join(installDir, "config"), 0755)
	os.MkdirAll(filepath.Join(installDir, "logs"), 0755)

	// Write config
	configPath := filepath.Join(installDir, "config", "device.json")
	configContent := fmt.Sprintf(`{
    "server_url": "%s",
    "device_key": "%s",
    "collect_interval": 30,
    "log_level": "info"
}`, *flagServer, deviceKey)

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		fmt.Printf("Warning: Failed to write config: %v\n", err)
	} else {
		fmt.Printf("Config: %s\n", configPath)
	}

	// Install as service
	if !*flagSkipService {
		fmt.Println()
		fmt.Println("Installing as service...")
		installService(osName, binaryPath, *flagServer, deviceKey)
	}

	fmt.Println()
	fmt.Println("======================================")
	fmt.Println("  Installation complete!")
	fmt.Println("======================================")
	fmt.Println()
	pauseIfWindows()
}

// pauseIfWindows keeps the console open when the installer is double-clicked
// on Windows, so users can read the output before the window closes.
func pauseIfWindows() {
	if runtime.GOOS == "windows" {
		fmt.Println()
		fmt.Print("Press Enter to exit...")
		fmt.Scanln()
	}
}

func printHelp() {
	fmt.Println("OurWay Agent Installer")
	fmt.Println()
	fmt.Println("Usage: ourway-installer [OPTIONS]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --server URL       OurWay server URL (default: http://localhost:8080)")
	fmt.Println("  --key KEY          Device key for authentication")
	fmt.Println("  --register         Register device with server and get key")
	fmt.Println("  --install-dir DIR  Installation directory")
	fmt.Println("  --skip-service     Don't install as a service")
	fmt.Println("  --version VER      Agent version to install")
	fmt.Println("  --help             Show this help")
}

func registerDevice(osName, arch string) string {
	hostname, _ := os.Hostname()

	// Build payload
	payload := fmt.Sprintf(`{"name":"%s","hostname":"%s","os":"%s","arch":"%s","agent_version":"%s"}`,
		hostname, hostname, osName, arch, version)

	url := strings.TrimRight(*flagServer, "/") + "/api/agent/register"

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", strings.NewReader(payload))
	if err != nil {
		fmt.Printf("Error registering device: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: Server returned %d: %s\n", resp.StatusCode, string(body))
		return ""
	}

	// Parse response for device_key
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return ""
	}

	if key, ok := result["device_key"].(string); ok {
		fmt.Printf("Device registered! Key: %s\n", key)
		return key
	}

	return ""
}

func installBinary(osName, arch, installDir, binName string) string {
	// 1. Use the agent binary embedded in this installer (self-contained).
	// The checked-in placeholder is tiny; a real embedded binary is >1KB.
	if len(embeddedAgent) > 1024 {
		fmt.Printf("Using embedded agent binary (%.1f MB)\n", float64(len(embeddedAgent))/1024/1024)
		binaryPath := filepath.Join(installDir, binName)
		if err := os.MkdirAll(installDir, 0755); err != nil {
			fmt.Printf("Error creating install dir: %v\n", err)
			return ""
		}
		if err := os.WriteFile(binaryPath, embeddedAgent, 0755); err != nil {
			fmt.Printf("Error writing embedded binary: %v\n", err)
			return ""
		}
		return binaryPath
	}

	// 2. Check for local binary first
	localPaths := []string{
		filepath.Join("dist", "agents", fmt.Sprintf("ourway-agent-%s-%s", osName, arch)),
		"./ourway-agent",
	}
	if osName == "windows" {
		localPaths = append(localPaths, filepath.Join("dist", "agents", fmt.Sprintf("ourway-agent-%s-%s.exe", osName, arch)), "./ourway-agent.exe")
	}

	for _, path := range localPaths {
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("Using local binary: %s\n", path)
			// Copy to install dir
			src, _ := os.ReadFile(path)
			binaryPath := filepath.Join(installDir, binName)
			os.MkdirAll(installDir, 0755)
			os.WriteFile(binaryPath, src, 0755)
			return binaryPath
		}
	}

	// 3. Download from server (fallback; the endpoint may not exist)
	binaryPath := filepath.Join(installDir, binName)
	os.MkdirAll(installDir, 0755)

	url := strings.TrimRight(*flagServer, "/") + "/api/agent/binary?os=" + osName + "&arch=" + arch
	if *flagVersion != "" {
		url += "&version=" + *flagVersion
	}

	fmt.Printf("Downloading from: %s\n", url)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("Error downloading binary: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Error: Server returned %d: %s\n", resp.StatusCode, string(body))
		return ""
	}

	out, err := os.Create(binaryPath)
	if err != nil {
		fmt.Printf("Error creating binary file: %v\n", err)
		return ""
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		fmt.Printf("Error writing binary: %v\n", err)
		return ""
	}

	os.Chmod(binaryPath, 0755)
	fmt.Println("Binary downloaded successfully")
	return binaryPath
}

func installService(osName, binaryPath, server, key string) {
	switch osName {
	case "linux":
		unit := fmt.Sprintf(`[Unit]
Description=OurWay RMM Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=%s --server %s --key %s
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, binaryPath, server, key)

		unitPath := "/etc/systemd/system/ourway-agent.service"
		if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
			fmt.Printf("Error writing service file: %v\n", err)
			return
		}

		run("systemctl", "daemon-reload")
		run("systemctl", "enable", "ourway-agent")
		run("systemctl", "start", "ourway-agent")
		fmt.Println("Service installed and started")

	case "darwin":
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.ourway.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--server</string>
        <string>%s</string>
        <string>--key</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>`, binaryPath, server, key)

		plistPath := "/Library/LaunchDaemons/com.ourway.agent.plist"
		if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
			fmt.Printf("Error writing plist: %v\n", err)
			return
		}

		run("launchctl", "load", "-w", plistPath)
		fmt.Println("Service installed and started")

	case "windows":
		// Use New-Service instead of sc.exe: the install dir contains a
		// space (C:\Program Files\OurWay\Agent), which breaks sc.exe
		// argument parsing when invoked through `powershell -Command`.
		// PowerShell single-quoted strings escape ' by doubling it.
		psq := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
		psScript := fmt.Sprintf(
			"New-Service -Name OurWayAgent -BinaryPathName '%s --server %s --key %s' -StartupType Automatic -DisplayName 'OurWay RMM Agent'; Start-Service OurWayAgent",
			psq(binaryPath), psq(server), psq(key),
		)
		run("powershell", "-NoProfile", "-Command", psScript)
		fmt.Println("Service installed and started")
	}
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Warning: %s %v failed: %v\n", name, args, err)
	}
}
