package collector

import (
	"github.com/shirou/gopsutil/v4/net"
)

// NetworkCollector collects network interface and connection metrics.
type NetworkCollector struct{}

// NewNetworkCollector creates a new network collector.
func NewNetworkCollector() *NetworkCollector {
	return &NetworkCollector{}
}

// Name returns the collector name.
func (c *NetworkCollector) Name() string {
	return "network"
}

// Collect gathers network interface and connection metrics.
func (c *NetworkCollector) Collect() (map[string]interface{}, error) {
	// Get network interface counters
	counters, err := net.IOCounters(false)
	if err != nil {
		return nil, err
	}

	interfaces := make(map[string]NetworkInfo)
	for _, counter := range counters {
		interfaces[counter.Name] = NetworkInfo{
			Name:        counter.Name,
			BytesSent:   counter.BytesSent,
			BytesRecv:   counter.BytesRecv,
			PacketsSent: counter.PacketsSent,
			PacketsRecv: counter.PacketsRecv,
		}
	}

	result := map[string]interface{}{
		"interfaces": interfaces,
	}

	// Count TCP connections (best effort)
	conns, err := net.Connections("tcp")
	if err == nil {
		result["tcp_connections"] = len(conns)
	}

	return result, nil
}
