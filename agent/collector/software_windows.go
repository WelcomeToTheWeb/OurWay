//go:build windows

package collector

import (
	"os/exec"
	"strings"
)

// collectSoftwarePackages collects installed packages on Windows.
func collectSoftwarePackages() (map[string]string, error) {
	packages := make(map[string]string)

	// Use PowerShell to get installed programs
	cmd := exec.Command("powershell", "-Command",
		"Get-ItemProperty HKLM:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\*, "+
			"HKLM:\\Software\\Wow6432Node\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\* | "+
			"Where-Object { $_.DisplayName -ne $null } | "+
			"Format-List DisplayName, DisplayVersion")
	
	if out, err := cmd.Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		var name, version string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "DisplayName:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "DisplayName:"))
			} else if strings.HasPrefix(line, "DisplayVersion:") {
				version = strings.TrimSpace(strings.TrimPrefix(line, "DisplayVersion:"))
				if name != "" {
					packages[name] = version
				}
			}
		}
	}

	return packages, nil
}
