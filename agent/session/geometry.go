package session

import (
	"log"
	"sync"
)

// The web client sends mouse coordinates as 0-100 percentages of the
// captured video frame, but platform input APIs expect absolute screen
// pixels. fetchDisplayGeometry (platform-specific, see geometry_*.go)
// reports the primary display size in pixels; displaySize caches it for
// the process lifetime so we never query the display on every mouse
// event. (A resolution change mid-session, e.g. hot-plugging a monitor,
// is not picked up; acceptable for a remote-control session.)
var (
	displaySizeOnce sync.Once
	displaySizeW    int
	displaySizeH    int
)

// displaySize returns the cached primary display size in pixels.
func displaySize() (w, h int) {
	displaySizeOnce.Do(func() {
		displaySizeW, displaySizeH = fetchDisplayGeometry()
		if displaySizeW <= 0 || displaySizeH <= 0 {
			log.Printf("session: display geometry unavailable; mouse coordinates will pass through unscaled")
		}
	})
	return displaySizeW, displaySizeH
}

// toScreenCoords converts 0-100 frame percentages into absolute screen
// pixels (rounded and clamped to the display). If the geometry is
// unknown the coordinates are returned unchanged.
func toScreenCoords(x, y float64) (int, int) {
	w, h := displaySize()
	if w <= 0 || h <= 0 {
		return int(x), int(y)
	}
	px := int(x/100.0*float64(w) + 0.5)
	py := int(y/100.0*float64(h) + 0.5)
	if px < 0 {
		px = 0
	}
	if px > w-1 {
		px = w - 1
	}
	if py < 0 {
		py = 0
	}
	if py > h-1 {
		py = h - 1
	}
	return px, py
}
