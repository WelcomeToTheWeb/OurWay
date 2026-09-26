//go:build darwin

package collector

import (
	"os/exec"
	"strings"
)

// EncryptionCollector gathers macOS disk encryption (FileVault) status.
type EncryptionCollector struct{}

// NewEncryptionCollector creates a new encryption collector.
func NewEncryptionCollector() *EncryptionCollector {
	return &EncryptionCollector{}
}

// Name returns the collector name.
func (c *EncryptionCollector) Name() string {
	return "encryption"
}

// Collect gathers FileVault encryption status.
func (c *EncryptionCollector) Collect() (map[string]interface{}, error) {
	result := map[string]interface{}{}

	cmd := exec.Command("fdesetup", "status")
	output, err := cmd.Output()
	if err != nil {
		return result, err
	}

	status := strings.TrimSpace(string(output))
	result["status"] = status

	if strings.Contains(status, "FileVault is On") {
		result["enabled"] = true
	} else if strings.Contains(status, "FileVault is Off") {
		result["enabled"] = false
	} else {
		result["enabled"] = false
	}

	return result, nil
}
