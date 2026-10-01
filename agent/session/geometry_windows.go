//go:build windows

package session

import (
	"log"
	"syscall"
)

// SM_CXSCREEN / SM_CYSCREEN are the Win32 system-metrics indices for the
// primary screen's width and height in pixels.
const (
	smCXScreen = 0
	smCYScreen = 1
)

var (
	user32Dll            = syscall.NewLazyDLL("user32.dll")
	getSystemMetricsProc = user32Dll.NewProc("GetSystemMetrics")
)

// fetchDisplayGeometry returns the primary screen size in pixels using
// GetSystemMetrics(SM_CXSCREEN/SM_CYSCREEN) from user32.dll.
func fetchDisplayGeometry() (int, int) {
	w, _, _ := getSystemMetricsProc.Call(uintptr(smCXScreen))
	h, _, _ := getSystemMetricsProc.Call(uintptr(smCYScreen))
	wi, hi := int(w), int(h)
	if wi <= 0 || hi <= 0 {
		log.Printf("session: GetSystemMetrics returned unexpected geometry %dx%d", wi, hi)
	}
	return wi, hi
}
