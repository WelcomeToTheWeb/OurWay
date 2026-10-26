//go:build linux

package session

// inputCapability reports whether this unit can synthesize input at all:
// xdotool must be installed and an X display reachable. "granted" means the
// tools are there, not that a particular window will accept the event.
func inputCapability() string {
	if _, err := xdotoolPath(); err != nil {
		return CapabilityUnavailable
	}
	return CapabilityGranted
}
