package collector

import (
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

// CPUCollector collects CPU usage metrics.
type CPUCollector struct {
	perCore bool
}

// NewCPUCollector creates a new CPU collector.
func NewCPUCollector() *CPUCollector {
	return &CPUCollector{
		perCore: true,
	}
}

// Name returns the collector name.
func (c *CPUCollector) Name() string {
	return "cpu"
}

// Collect gathers CPU usage metrics.
func (c *CPUCollector) Collect() (map[string]interface{}, error) {
	// Get overall CPU percent with a small interval for non-blocking behavior
	percents, err := cpu.Percent(1*time.Second, false)
	if err != nil {
		return nil, err
	}

	overall := float64(0)
	if len(percents) > 0 {
		overall = percents[0]
	}

	var perCore []float64
	if c.perCore {
		corePercents, err := cpu.Percent(0, true)
		if err == nil {
			perCore = corePercents
		}
	}

	return map[string]interface{}{
		"percent":  overall,
		"per_core": perCore,
	}, nil
}
