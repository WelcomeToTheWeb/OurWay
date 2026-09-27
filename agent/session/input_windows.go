//go:build windows

package session

import "log"

// synthesizeKey is a stub on Windows.
func synthesizeKey(key, event string) {
	log.Printf("session: input synthesis not implemented on this platform (windows): key %q %s", key, event)
	// TODO: synthesize key events on Windows (e.g. via SendInput/win32).
}

// synthesizeMouse is a stub on Windows.
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	log.Printf("session: input synthesis not implemented on this platform (windows): %s at (%.0f, %.0f)", event, x, y)
	// TODO: synthesize mouse events on Windows (e.g. via SendInput/win32).
}
