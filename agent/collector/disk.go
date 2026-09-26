package collector

import (
	"github.com/shirou/gopsutil/v4/disk"
)

// DiskCollector collects disk usage and I/O metrics.
type DiskCollector struct{}

// NewDiskCollector creates a new disk collector.
func NewDiskCollector() *DiskCollector {
	return &DiskCollector{}
}

// Name returns the collector name.
func (c *DiskCollector) Name() string {
	return "disk"
}

// Collect gathers disk partition and I/O metrics.
func (c *DiskCollector) Collect() (map[string]interface{}, error) {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return nil, err
	}

	var diskInfos []DiskInfo
	for _, p := range partitions {
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}

		diskInfos = append(diskInfos, DiskInfo{
			MountPoint:   p.Mountpoint,
			Device:       p.Device,
			Type:         p.Fstype,
			Total:        usage.Total,
			Used:         usage.Used,
			Free:         usage.Free,
			UsagePercent: usage.UsedPercent,
		})
	}

	result := map[string]interface{}{
		"partitions": diskInfos,
	}

	// Disk I/O counters (best effort)
	ioCounters, err := disk.IOCounters()
	if err == nil {
		result["io"] = ioCounters
	}

	return result, nil
}
