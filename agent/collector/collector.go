package collector

import (
	"log"
)

// Collector is the interface that all metric collectors must implement.
type Collector interface {
	// Name returns the unique name of this collector.
	Name() string
	// Collect gathers metrics and returns them as a map.
	Collect() (map[string]interface{}, error)
}

// DiskInfo represents information about a disk partition.
type DiskInfo struct {
	MountPoint   string  `json:"mount_point"`
	Device       string  `json:"device"`
	Type         string  `json:"type"`
	Total        uint64  `json:"total"`
	Used         uint64  `json:"used"`
	Free         uint64  `json:"free"`
	UsagePercent float64 `json:"usage_percent"`
}

// NetworkInfo represents network interface statistics.
type NetworkInfo struct {
	Name        string `json:"name"`
	BytesSent   uint64 `json:"bytes_sent"`
	BytesRecv   uint64 `json:"bytes_recv"`
	PacketsSent uint64 `json:"packets_sent"`
	PacketsRecv uint64 `json:"packets_recv"`
}

// ProcessInfo represents information about a running process.
type ProcessInfo struct {
	PID    int32   `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu"`
	Memory uint64  `json:"memory"`
}

// Metrics holds all collected system metrics.
type Metrics struct {
	CPU          float64                `json:"cpu"`
	CPUPerCore   []float64              `json:"cpu_per_core"`
	RAM          float64                `json:"ram"`
	RAMUsed      uint64                 `json:"ram_used"`
	RAMTotal     uint64                 `json:"ram_total"`
	Swap         float64                `json:"swap"`
	SwapUsed     uint64                 `json:"swap_used"`
	SwapTotal    uint64                 `json:"swap_total"`
	Disks        []DiskInfo             `json:"disks"`
	Network      map[string]NetworkInfo `json:"network"`
	TopProcesses []ProcessInfo          `json:"top_processes"`
	Uptime       uint64                 `json:"uptime"`
	LoadAvg      []float64              `json:"load_avg"`
}

// CollectorManager manages all registered collectors.
type CollectorManager struct {
	collectors []Collector
}

// NewCollectorManager creates a new collector manager with all default collectors.
func NewCollectorManager() *CollectorManager {
	collectors := []Collector{
		NewCPUCollector(),
		NewMemoryCollector(),
		NewDiskCollector(),
		NewNetworkCollector(),
		NewProcessCollector(),
	}

	// Platform-specific collectors
	collectors = appendPlatformCollectors(collectors)

	return &CollectorManager{
		collectors: collectors,
	}
}

// CollectAll runs all collectors and aggregates the results into a Metrics struct.
func (cm *CollectorManager) CollectAll() (*Metrics, error) {
	metrics := &Metrics{}

	for _, c := range cm.collectors {
		data, err := c.Collect()
		if err != nil {
			// One flaky collector (e.g., a protected process on
			// Windows) must not drop the entire metrics batch.
			log.Printf("collector %s failed: %v", c.Name(), err)
			continue
		}

		switch c.Name() {
		case "cpu":
			if v, ok := data["percent"].(float64); ok {
				metrics.CPU = v
			}
			if v, ok := data["per_core"].([]float64); ok {
				metrics.CPUPerCore = v
			}
		case "memory":
			if v, ok := data["percent"].(float64); ok {
				metrics.RAM = v
			}
			if v, ok := data["used"].(uint64); ok {
				metrics.RAMUsed = v
			}
			if v, ok := data["total"].(uint64); ok {
				metrics.RAMTotal = v
			}
			if v, ok := data["swap_percent"].(float64); ok {
				metrics.Swap = v
			}
			if v, ok := data["swap_used"].(uint64); ok {
				metrics.SwapUsed = v
			}
			if v, ok := data["swap_total"].(uint64); ok {
				metrics.SwapTotal = v
			}
		case "disk":
			if v, ok := data["partitions"].([]DiskInfo); ok {
				metrics.Disks = v
			}
		case "network":
			if v, ok := data["interfaces"].(map[string]NetworkInfo); ok {
				metrics.Network = v
			}
		case "processes":
			if v, ok := data["top_cpu"].([]ProcessInfo); ok {
				metrics.TopProcesses = v
			}
		}
	}

	// Collect uptime
	uptime, err := Uptime()
	if err == nil {
		metrics.Uptime = uptime
	}

	// Collect load average (best effort)
	load, err := LoadAvg()
	if err == nil {
		metrics.LoadAvg = load
	}

	return metrics, nil
}
