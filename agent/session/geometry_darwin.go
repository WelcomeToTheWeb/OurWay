//go:build darwin && cgo

package session

/*
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

// fetchDisplayGeometry returns the main display size in pixels using the
// CoreGraphics CGDisplayPixelsWide/CGDisplayPixelsHeight APIs.
func fetchDisplayGeometry() (int, int) {
	display := C.CGMainDisplayID()
	w := int(C.CGDisplayPixelsWide(display))
	h := int(C.CGDisplayPixelsHeight(display))
	return w, h
}
