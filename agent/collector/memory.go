package collector

import (
	"github.com/shirou/gopsutil/v4/mem"
)

// MemoryCollector collects RAM and swap usage metrics.
type MemoryCollector struct{}

// NewMemoryCollector creates a new memory collector.
func NewMemoryCollector() *MemoryCollector {
	return &MemoryCollector{}
}

// Name returns the collector name.
func (c *MemoryCollector) Name() string {
	return "memory"
}

// Collect gathers memory usage metrics.
func (c *MemoryCollector) Collect() (map[string]interface{}, error) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"percent": vm.UsedPercent,
		"used":    vm.Used,
		"total":   vm.Total,
		"free":    vm.Available,
	}

	// Swap is optional and may not be available on all systems
	swap, err := mem.SwapMemory()
	if err == nil && swap != nil {
		result["swap_percent"] = swap.UsedPercent
		result["swap_used"] = swap.Used
		result["swap_total"] = swap.Total
	} else {
		result["swap_percent"] = 0.0
		result["swap_used"] = uint64(0)
		result["swap_total"] = uint64(0)
	}

	return result, nil
}
