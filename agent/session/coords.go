package session

import "image"

// Monitor describes one display in virtual-desktop pixel coordinates.
type Monitor struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Primary bool   `json:"primary"`
}

// Rect returns the monitor's rectangle.
func (m Monitor) Rect() image.Rectangle {
	return image.Rect(m.X, m.Y, m.X+m.W, m.Y+m.H)
}

// absVirtualCoords converts a position given as percentages (0-100) of
// region into the 0-65535 range SendInput expects with
// MOUSEEVENTF_VIRTUALDESK, where 0..65535 spans the whole virtual
// desktop (virt), not just the primary monitor.
func absVirtualCoords(region, virt image.Rectangle, xPct, yPct float64) (int32, int32) {
	px := float64(region.Min.X) + clampPct(xPct)/100*float64(region.Dx()-1)
	py := float64(region.Min.Y) + clampPct(yPct)/100*float64(region.Dy()-1)
	return scaleAbs(px, virt.Min.X, virt.Dx()), scaleAbs(py, virt.Min.Y, virt.Dy())
}

func scaleAbs(p float64, origin, size int) int32 {
	if size <= 1 {
		return 0
	}
	v := (p - float64(origin)) * 65535 / float64(size-1)
	if v < 0 {
		v = 0
	}
	if v > 65535 {
		v = 65535
	}
	return int32(v + 0.5)
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
