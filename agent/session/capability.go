package session

import "sync/atomic"

// What this unit can do, reported to the server with every metrics sample so
// the console can answer "why is control inert?" before a technician finds out
// in front of a live machine.
//
// Both values are additive on the wire: an agent that does not send them leaves
// the server's stored value alone, and "unknown" is what the console shows for
// a unit that has never reported. Unknown is never presented as granted.
const (
	CaptureStateUnknown = "unknown"
	CaptureStateOK      = "ok"
	CaptureStateError   = "error"
)

var captureState atomic.Value

func init() {
	captureState.Store(CaptureStateUnknown)
}

// setCaptureState records the last capture outcome (called from the capture loop).
func setCaptureState(state string) {
	captureState.Store(state)
}

// CaptureState reports the last capture outcome this agent observed.
func CaptureState() string {
	if v, ok := captureState.Load().(string); ok && v != "" {
		return v
	}
	return CaptureStateUnknown
}

// RequestInputGrant asks the platform to show its remote-input grant prompt
// (macOS Accessibility). It is a no-op elsewhere.
func RequestInputGrant() { requestInputGrant() }

// InputCapability reports whether remote input can land on this unit:
// "granted", "denied" (macOS without Accessibility), "unavailable" (no tooling
// or a build without the capability), "unknown" (never checked).
func InputCapability() string {
	return inputCapability()
}
