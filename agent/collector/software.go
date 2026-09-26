package collector

// CollectSoftwarePackages returns a map of installed package names to versions.
// This is used for patch management and software inventory.
func CollectSoftwarePackages() (map[string]string, error) {
	return collectSoftwarePackages()
}
