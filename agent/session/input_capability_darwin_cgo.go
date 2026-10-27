//go:build darwin && cgo

package session

// inputCapability reports the Accessibility (TCC) state for this binary.
//
// "granted" is the only value that means remote input lands. "denied" means the
// system is discarding the events; the console must say that, because the
// session looks perfectly live while every keystroke is dropped.
func inputCapability() string {
	if accessibilityGranted() {
		return CapabilityGranted
	}
	return CapabilityDenied
}
