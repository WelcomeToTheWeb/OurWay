//go:build darwin

package collector

import (
	"os/exec"
	"strings"
)

// collectSoftwarePackages collects installed packages on macOS.
func collectSoftwarePackages() (map[string]string, error) {
	packages := make(map[string]string)

	// Try pkgutil
	if out, err := exec.Command("pkgutil", "--pkgs").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			pkg := strings.TrimSpace(line)
			if pkg != "" {
				packages[pkg] = "installed"
			}
		}
		return packages, nil
	}

	return packages, nil
}
