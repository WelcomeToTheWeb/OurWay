//go:build darwin

package session

import "log"

// synthesizeKey is a stub on macOS.
func synthesizeKey(key, event string) {
	log.Printf("session: input synthesis not implemented on this platform (darwin): key %q %s", key, event)
	// TODO: synthesize key events on macOS (e.g. via CGEventCreateKeyboardEvent).
}

// synthesizeMouse is a stub on macOS.
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	log.Printf("session: input synthesis not implemented on this platform (darwin): %s at (%.0f, %.0f)", event, x, y)
	// TODO: synthesize mouse events on macOS (e.g. via CGEventCreateMouseEvent).
}
