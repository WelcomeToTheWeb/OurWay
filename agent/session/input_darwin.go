//go:build darwin

package session

import "log"

// synthesizeKey is a stub on macOS.
func synthesizeKey(key, event string) {
	log.Printf("session: input synthesis not implemented on this platform (darwin): key %q %s", key, event)
	// TODO: synthesize key events on macOS (e.g. via CGEventCreateKeyboardEvent).
}

// synthesizeMouse is a stub on macOS. x and y arrive as 0-100
// percentages of the captured frame (the web client contract) and are
// converted to absolute screen pixels so a future implementation can
// pass them straight to the CGEvent APIs.
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	px, py := toScreenCoords(x, y)
	log.Printf("session: input synthesis not implemented on this platform (darwin): %s at (%d, %d)", event, px, py)
	// TODO: synthesize mouse events on macOS (e.g. via CGEventCreateMouseEvent).
}
