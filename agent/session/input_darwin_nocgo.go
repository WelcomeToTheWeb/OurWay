//go:build darwin && !cgo

package session

import "log"

// The darwin release binaries are cross-compiled with CGO_ENABLED=0, and CGEvent
// posting needs CoreGraphics. A non-cgo darwin build therefore CANNOT synthesize
// input — and the whole point of this file is that it says so, in the capability
// the console shows, instead of accepting events and discarding them silently
// (buglist.md H8, the finding this task closes).
//
// macOS units that must be remotely controlled need the cgo build, which is
// produced on a macOS builder (CGO_ENABLED=1). docs/agent.md states the
// requirement, the Accessibility grant, and the fixed-path/codesign rule.

// synthesizeKey reports that this build cannot control input.
func synthesizeKey(key, code, event string, mods inputModifiers) {
	logThrottled("input-unavailable", "remote key input unavailable: this agent was built without cgo (no CoreGraphics); key "+key+" dropped")
}

// synthesizeMouse reports that this build cannot control input.
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	logThrottled("input-unavailable", "remote mouse input unavailable: this agent was built without cgo (no CoreGraphics); "+event+" dropped")
}

// inputCapability tells the console the truth about this build.
func inputCapability() string { return CapabilityUnavailable }

// requestInputGrant has nothing to prompt from in a non-cgo build.
func requestInputGrant() {
	log.Println("session: input grant prompt requires the cgo agent build (see docs/agent.md)")
}
