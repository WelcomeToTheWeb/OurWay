//go:build windows

package session

import (
	"fmt"
	"image"
	"sync"
	"syscall"
	"unsafe"
)

var (
	procEnumDisplayMonitors = user32DLL.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32DLL.NewProc("GetMonitorInfoW")

	procSetProcessDpiAwarenessContext = user32DLL.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = user32DLL.NewProc("SetProcessDPIAware")
)

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	monitorInfoPrimary = 0x1
)

// EnableDPIAwareness makes this process per-monitor DPI aware. Without
// it Windows virtualizes coordinates on scaled displays, so capture
// sizes, GetSystemMetrics and SendInput positions disagree with the
// physical pixels Desktop Duplication returns. Call it once, first
// thing in the remote-control exe.
func EnableDPIAwareness() {
	const perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT -4
	if r, _, _ := procSetProcessDpiAwarenessContext.Call(perMonitorAwareV2); r != 0 {
		return
	}
	procSetProcessDPIAware.Call()
}

type rect32 struct{ Left, Top, Right, Bottom int32 }

// monitorInfoEx mirrors Win32 MONITORINFOEXW.
type monitorInfoEx struct {
	Size    uint32
	Monitor rect32
	Work    rect32
	Flags   uint32
	Device  [32]uint16
}

var (
	enumMu   sync.Mutex
	enumOut  []Monitor
	enumProc = syscall.NewCallback(func(hMon, hdc, lprc, lParam uintptr) uintptr {
		var mi monitorInfoEx
		mi.Size = uint32(unsafe.Sizeof(mi))
		if r, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi))); r == 0 {
			return 1
		}
		enumOut = append(enumOut, Monitor{
			ID:      len(enumOut),
			Name:    syscall.UTF16ToString(mi.Device[:]),
			X:       int(mi.Monitor.Left),
			Y:       int(mi.Monitor.Top),
			W:       int(mi.Monitor.Right - mi.Monitor.Left),
			H:       int(mi.Monitor.Bottom - mi.Monitor.Top),
			Primary: mi.Flags&monitorInfoPrimary != 0,
		})
		return 1 // continue enumeration
	})
)

// enumMonitors lists the attached displays; IDs are enumeration
// indexes and are only stable while the layout does not change.
func enumMonitors() []Monitor {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumOut = nil
	procEnumDisplayMonitors.Call(0, 0, enumProc, 0)
	out := enumOut
	enumOut = nil
	if len(out) == 0 {
		w, h := fetchDisplayGeometry()
		out = []Monitor{{ID: 0, Name: "Display", W: w, H: h, Primary: true}}
	}
	return out
}

// virtualScreenRect is the bounding rectangle of all monitors.
func virtualScreenRect() image.Rectangle {
	g := func(i uintptr) int {
		v, _, _ := getSystemMetricsProc.Call(i)
		return int(int32(v)) // origin can be negative
	}
	x, y := g(smXVirtualScreen), g(smYVirtualScreen)
	return image.Rect(x, y, x+g(smCXVirtualScreen), y+g(smCYVirtualScreen))
}

// gdiCaptureRGBA grabs rect (virtual-desktop coordinates) with BitBlt.
func gdiCaptureRGBA(rect image.Rectangle) (*image.RGBA, error) {
	w, h := rect.Dx(), rect.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid capture rect %v", rect)
	}
	hScreen, _, _ := gdiGetDCProc.Call(0)
	if hScreen == 0 {
		return nil, fmt.Errorf("GetDC(0) failed")
	}
	defer gdiReleaseDCProc.Call(0, hScreen)
	hDC, _, _ := gdiCreateCompatibleDCProc.Call(hScreen)
	if hDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer gdiDeleteDCProc.Call(hDC)
	hBmp, _, _ := gdiCreateCompatBmpProc.Call(hScreen, uintptr(w), uintptr(h))
	if hBmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer gdiDeleteObjectProc.Call(hBmp)
	old, _, serr := gdiSelectObjectProc.Call(hDC, hBmp)
	if old == 0 {
		return nil, fmt.Errorf("SelectObject failed: %v", serr)
	}
	// BitBlt takes the source origin as a signed int; negative
	// virtual coordinates must wrap into a uintptr like int32 does.
	ok, _, berr := gdiBitBltProc.Call(hDC, 0, 0, uintptr(w), uintptr(h), hScreen,
		uintptr(int32(rect.Min.X)), uintptr(int32(rect.Min.Y)), gdiSrcCopy)
	gdiSelectObjectProc.Call(hDC, old)
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt failed: %v", berr)
	}
	pixels, err := gdiCaptureToBGRA(hDC, hBmp, w, h)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(pixels); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], 0xff
	}
	return img, nil
}
