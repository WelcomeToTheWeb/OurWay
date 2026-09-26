//go:build linux

package collector

import (
	"os/exec"
	"strings"
)

// collectSoftwarePackages collects installed packages on Linux.
func collectSoftwarePackages() (map[string]string, error) {
	packages := make(map[string]string)

	// Try dpkg first (Debian/Ubuntu)
	if out, err := exec.Command("dpkg-query", "-W", "-f", "${Package} ${Version}\n").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				packages[parts[0]] = parts[1]
			}
		}
		return packages, nil
	}

	// Try rpm (RHEL/CentOS/Fedora)
	if out, err := exec.Command("rpm", "-qa", "--queryformat", "%{NAME} %{VERSION}\n").Output(); err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				packages[parts[0]] = parts[1]
			}
		}
		return packages, nil
	}

	return packages, nil
}
