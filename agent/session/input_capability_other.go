//go:build !linux && !windows && !darwin

package session

// inputCapability on platforms with no remote-input implementation.
func inputCapability() string { return CapabilityUnavailable }
