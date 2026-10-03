package collector

import (
	"github.com/shirou/gopsutil/v4/cpu"
)

// CPUCollector collects CPU usage metrics.
type CPUCollector struct {
	perCore bool
}

// NewCPUCollector creates a new CPU collector. The Percent(0) call
// primes gopsutil's tick cache so the first Collect() reports a real
// delta instead of zero.
func NewCPUCollector() *CPUCollector {
	_, _ = cpu.Percent(0, false)
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
	// L1: zero-interval sampling computes the delta since the previous
	// call instead of blocking for 1 s — the blocking sample consumed
	// half of the 2 s streaming budget.
	percents, err := cpu.Percent(0, false)
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
