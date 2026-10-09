//go:build darwin && !cgo

package session

import "log"

// synthesizeKey is a stub when cgo is disabled (cross-compiled release
// builds). A cgo build enables the CGEvent implementation
// (input_darwin.go); see geometry_darwin.go for the same split.
func synthesizeKey(key, event string) {
	log.Printf("session: input synthesis requires a cgo build on darwin; key %q %s dropped", key, event)
}

// synthesizeMouse is a stub when cgo is disabled. x and y arrive as 0-100
// percentages of the captured frame and are converted to absolute screen
// pixels so a cgo build can pass them straight to CGEventCreateMouseEvent.
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	px, py := toScreenCoords(x, y)
	log.Printf("session: input synthesis requires a cgo build on darwin; %s at (%d, %d) dropped", event, px, py)
}
