//go:build darwin

package collector

import (
	"os/exec"
	"strings"
)

// SoftwareCollector gathers installed software information on macOS.
type SoftwareCollector struct{}

// NewSoftwareCollector creates a new software collector.
func NewSoftwareCollector() *SoftwareCollector {
	return &SoftwareCollector{}
}

// Name returns the collector name.
func (c *SoftwareCollector) Name() string {
	return "software"
}

// SoftwareInfo represents installed software.
type SoftwareInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Collect gathers installed software inventory.
func (c *SoftwareCollector) Collect() (map[string]interface{}, error) {
	result := map[string]interface{}{}

	// Get OS version
	cmd := exec.Command("sw_vers")
	output, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				if key == "ProductName" {
					result["os_name"] = value
				} else if key == "ProductVersion" {
					result["os_version"] = value
				}
			}
		}
	}

	// Get kernel version
	cmd = exec.Command("uname", "-r")
	output, err = cmd.Output()
	if err == nil {
		result["kernel"] = strings.TrimSpace(string(output))
	}

	// Count installed applications
	cmd = exec.Command("ls", "/Applications")
	output, err = cmd.Output()
	if err == nil {
		apps := strings.Split(strings.TrimSpace(string(output)), "\n")
		result["app_count"] = len(apps)
	}

	return result, nil
}
