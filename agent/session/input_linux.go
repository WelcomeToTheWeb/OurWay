//go:build linux

package session

import (
	"fmt"
	"log"
	"os/exec"
)

// xdotoolKeyMap maps a few browser KeyboardEvent.key values to the key
// names xdotool expects (the two naming schemes differ).
var xdotoolKeyMap = map[string]string{
	"Enter":     "Return",
	" ":         "space",
	"Backspace": "BackSpace",
	"Control":   "ctrl",
	"Meta":      "super",
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
func synthesizeMouse(event string, x, y float64, button string, delta float64) {
	bin, err := xdotoolPath()
	if err != nil {
		log.Printf("session: xdotool not found; cannot synthesize mouse %q (install xdotool)", event)
		return
	}
	switch event {
	case "move":
		if err := exec.Command(bin, "mousemove", fmt.Sprintf("%.0f", x), fmt.Sprintf("%.0f", y)).Run(); err != nil {
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
