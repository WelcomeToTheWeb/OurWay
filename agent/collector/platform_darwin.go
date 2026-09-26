//go:build darwin

package collector

// appendPlatformCollectors adds macOS-specific collectors.
func appendPlatformCollectors(collectors []Collector) []Collector {
	return append(collectors,
		NewEnergyCollector(),
		NewEncryptionCollector(),
		NewSoftwareCollector(),
	)
}
