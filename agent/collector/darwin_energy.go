//go:build darwin

package collector

import (
	"fmt"
	"os/exec"
	"strings"
)

// EnergyCollector gathers macOS power and energy metrics.
type EnergyCollector struct{}

// NewEnergyCollector creates a new energy collector.
func NewEnergyCollector() *EnergyCollector {
	return &EnergyCollector{}
}

// Name returns the collector name.
func (c *EnergyCollector) Name() string {
	return "energy"
}

// Collect gathers energy metrics from macOS.
func (c *EnergyCollector) Collect() (map[string]interface{}, error) {
	result := map[string]interface{}{}

	// Get battery info using pmset
	cmd := exec.Command("pmset", "-g", "batt")
	output, err := cmd.Output()
	if err != nil {
		return result, fmt.Errorf("failed to get battery info: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse "Now drawing from 'Battery Power'" or "AC Power"
		if strings.Contains(line, "Now drawing from") {
			powerSource := strings.TrimPrefix(line, "Now drawing from '")
			powerSource = strings.TrimSuffix(powerSource, "'")
			result["power_source"] = powerSource
		}

		// Parse battery percentage and time remaining
		if strings.Contains(line, "Battery Power") && strings.Contains(line, "%") {
			// Example: "Battery Power 85% remaining (7:30 estimated)"
			parts := strings.Split(line, " ")
			for i, part := range parts {
				if strings.HasSuffix(part, "%") {
					percent := strings.TrimSuffix(part, "%")
					result["battery_percent"] = percent
					if i+2 < len(parts) && strings.Contains(parts[i+2], "estimated") {
						timeRemaining := parts[i+1]
						timeRemaining = strings.Trim(timeRemaining, "()")
						result["time_remaining"] = timeRemaining
					}
				}
			}
		}
	}

	// Get system power metrics (CPU power, etc.)
	cmd = exec.Command("ioreg", "-c", "AppleSmartBattery")
	output, err = cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "MaxCapacity") {
				result["max_capacity"] = strings.Split(line, "\t")[1]
			}
			if strings.Contains(line, "CurrentCapacity") {
				result["current_capacity"] = strings.Split(line, "\t")[1]
			}
			if strings.Contains(line, "CycleCount") {
				result["cycle_count"] = strings.Split(line, "\t")[1]
			}
			if strings.Contains(line, "DesignCapacity") {
				result["design_capacity"] = strings.Split(line, "\t")[1]
			}
		}
	}

	return result, nil
}
