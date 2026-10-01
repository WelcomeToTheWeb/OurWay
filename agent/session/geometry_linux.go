//go:build linux

package session

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// fetchDisplayGeometry returns the primary display size in pixels using
// `xdotool getdisplaygeometry`, which prints "W H".
func fetchDisplayGeometry() (int, int) {
	out, err := exec.Command("xdotool", "getdisplaygeometry").Output()
	if err != nil {
		log.Printf("session: xdotool getdisplaygeometry failed: %v", err)
		return 0, 0
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		log.Printf("session: unexpected getdisplaygeometry output %q", strings.TrimSpace(string(out)))
		return 0, 0
	}
	w, errW := strconv.Atoi(fields[0])
	h, errH := strconv.Atoi(fields[1])
	if errW != nil || errH != nil {
		log.Printf("session: unexpected getdisplaygeometry output %q", strings.TrimSpace(string(out)))
		return 0, 0
	}
	return w, h
}
