package collector

import (
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/host"
)

// Uptime returns the system uptime in seconds.
func Uptime() (uint64, error) {
	return host.Uptime()
}

// LoadAvg returns the system load averages (1, 5, 15 minute).
func LoadAvg() ([]float64, error) {
	avg, err := load.Avg()
	if err != nil {
		return nil, err
	}
	return []float64{avg.Load1, avg.Load5, avg.Load15}, nil
}
