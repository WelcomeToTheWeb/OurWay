//go:build darwin && !cgo

package session

import (
	"log"
	"os/exec"
	"regexp"
	"strconv"
)

// CGDisplayPixelsWide/Height require cgo, which is disabled when the
// darwin release binaries are cross-compiled. Fall back to the
// (logical) resolution reported by system_profiler. Note that on Retina
// displays this is in points, not pixels; a cgo build is required for
// pixel-accurate geometry.
var resolutionRe = regexp.MustCompile(`Resolution:\s*(\d+)\s*x\s*(\d+)`)

// fetchDisplayGeometry returns the main display size using
// `system_profiler SPDisplaysDataType`.
func fetchDisplayGeometry() (int, int) {
	out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output()
	if err != nil {
		log.Printf("session: system_profiler SPDisplaysDataType failed: %v", err)
		return 0, 0
	}
	m := resolutionRe.FindSubmatch(out)
	if m == nil {
		log.Printf("session: could not parse display resolution from system_profiler output")
		return 0, 0
	}
	w, errW := strconv.Atoi(string(m[1]))
	h, errH := strconv.Atoi(string(m[2]))
	if errW != nil || errH != nil {
		log.Printf("session: could not parse display resolution %q", string(m[0]))
		return 0, 0
	}
	log.Printf("session: using logical resolution %dx%d fallback (cgo disabled)", w, h)
	return w, h
}
