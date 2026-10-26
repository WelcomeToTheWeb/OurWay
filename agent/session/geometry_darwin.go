//go:build darwin && cgo

package session

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>

// The size of the main display, read out of its bounds rectangle.
//
// CGDisplayPixelsWide/CGDisplayPixelsHeight look like the obvious calls, and
// cgo cannot compile them: `CGDisplayPixelsHeight` sits behind a deployment-
// target conditional in the SDK headers, and cgo's parser refuses to
// evaluate it ("could not determine what C.CGDisplayPixelsHeight refers to").
// Reading the geometry from CGDisplayBounds — one API, both dimensions, no
// version conditional — is what compiles, and a C helper keeps the version
// conditionals out of cgo's way entirely.
static void main_display_size(int *width, int *height) {
  CGRect bounds = CGDisplayBounds(CGMainDisplayID());
  *width = (int)bounds.size.width;
  *height = (int)bounds.size.height;
}
*/
import "C"

// fetchDisplayGeometry returns the main display size in pixels.
func fetchDisplayGeometry() (int, int) {
	var w, h C.int
	C.main_display_size(&w, &h)
	return int(w), int(h)
}
