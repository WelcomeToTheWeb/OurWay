//go:build linux

package session

import (
	"log"
	"os/exec"
	"strconv"
)

// xdotoolKeyMap maps browser KeyboardEvent.key values to the key names
// xdotool expects (the two naming schemes differ).
var xdotoolKeyMap = map[string]string{
	"Enter":       "Return",
	" ":           "space",
	"Backspace":   "BackSpace",
	"Tab":         "Tab",
	"Escape":      "Escape",
	"Delete":      "Delete",
	"Insert":      "Insert",
	"ArrowUp":     "Up",
	"ArrowDown":   "Down",
	"ArrowLeft":   "Left",
	"ArrowRight":  "Right",
	"PageUp":      "Prior",
	"PageDown":    "Next",
	"Home":        "Home",
	"End":         "End",
	"Control":     "ctrl",
	"Shift":       "shift",
	"Alt":         "alt",
	"Meta":        "super",
	"CapsLock":    "Caps_Lock",
	"PrintScreen": "Print",
	"Pause":       "Pause",
	"ScrollLock":  "Scroll_Lock",
	"NumLock":     "Num_Lock",
	"F1":          "F1", "F2": "F2", "F3": "F3", "F4": "F4",
	"F5": "F5", "F6": "F6", "F7": "F7", "F8": "F8",
	"F9": "F9", "F10": "F10", "F11": "F11", "F12": "F12",
}

func xdotoolPath() (string, error) {
	return exec.LookPath("xdotool")
}

// synthesizeKey presses (or releases) a key using xdotool.
// It is a no-op with a log line if xdotool is not installed.
func synthesizeKey(key, event string) {
	bin, err := xdotoolPath()
	if err != nil {
		log.Printf("session: xdotool not found; cannot synthesize key %q (install xdotool)", key)
		return
	}
	if mapped, ok := xdotoolKeyMap[key]; ok {
		key = mapped
	}
	args := []string{"key", key}
	switch event {
	case "down":
		args = []string{"keydown", key}
	case "up":
		args = []string{"keyup", key}
	}
	if err := exec.Command(bin, args...).Run(); err != nil {
		log.Printf("session: xdotool %v failed: %v", args, err)
	}
}

// synthesizeMouse moves the pointer or clicks/scrolls using xdotool.
// It is a no-op with a log line if xdotool is not installed.
// x and y are 0-100 percentages of the captured frame (the web client
// contract) and are converted to absolute screen pixels here, because
// xdotool mousemove expects screen coordinates.
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	px, py := toScreenCoords(x, y)
	bin, err := xdotoolPath()
	if err != nil {
		log.Printf("session: xdotool not found; cannot synthesize mouse %q (install xdotool)", event)
		return
	}
	switch event {
	case "move":
		if err := exec.Command(bin, "mousemove", strconv.Itoa(px), strconv.Itoa(py)).Run(); err != nil {
			log.Printf("session: xdotool mousemove failed: %v", err)
		}
	case "click":
		btn := "1" // left
		if button == "right" {
			btn = "3"
		}
		if err := exec.Command(bin, "click", btn).Run(); err != nil {
			log.Printf("session: xdotool click failed: %v", err)
		}
	case "scroll":
		btn := "4" // scroll up
		if delta > 0 {
			btn = "5" // scroll down
		}
		if err := exec.Command(bin, "click", btn).Run(); err != nil {
			log.Printf("session: xdotool scroll failed: %v", err)
		}
	}
}
