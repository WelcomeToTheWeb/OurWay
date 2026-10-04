//go:build !windows

package session

// InjectKey synthesizes a key event for the remote-control executable
// (see the Windows variant for why it is exported).
func InjectKey(key, event string) {
	synthesizeKey(key, event)
}

// InjectMouse synthesizes a mouse event for the remote-control
// executable.
func InjectMouse(event string, x, y float64, button string, delta float64) {
	synthesizeMouse(event, x, y, button, delta)
}
