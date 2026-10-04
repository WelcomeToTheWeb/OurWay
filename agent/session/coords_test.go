package session

import (
	"image"
	"testing"
)

func TestAbsVirtualCoords(t *testing.T) {
	// Two 1920x1080 monitors; the secondary sits left of the primary,
	// so the virtual desktop starts at x=-1920.
	virt := image.Rect(-1920, 0, 1920, 1080)
	left := image.Rect(-1920, 0, 0, 1080)
	primary := image.Rect(0, 0, 1920, 1080)

	x, y := absVirtualCoords(left, virt, 0, 0)
	if x != 0 || y != 0 {
		t.Errorf("left monitor origin = (%d,%d), want (0,0)", x, y)
	}
	x, _ = absVirtualCoords(primary, virt, 100, 50)
	if x != 65535 {
		t.Errorf("right edge of primary = %d, want 65535", x)
	}
	x, _ = absVirtualCoords(primary, virt, 0, 0)
	if x < 32767 || x > 32768+20 { // virtual x=0 is just past the middle
		t.Errorf("primary origin x = %d, want ~half scale", x)
	}
	// Out-of-range percentages are clamped to the monitor.
	if x, _ := absVirtualCoords(left, virt, -50, 0); x != 0 {
		t.Errorf("negative pct not clamped: %d", x)
	}
}
