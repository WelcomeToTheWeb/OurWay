//go:build windows

package session

// inputCapability on Windows: SendInput needs no user grant, so a running
// agent with a desktop session can control input. A session-0/service desktop
// is the exception, and it shows up as events that go nowhere; the capability
// field is the place to surface that once it is measured.
func inputCapability() string { return CapabilityGranted }
