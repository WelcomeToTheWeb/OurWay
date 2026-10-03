//go:build windows

package session

import (
	"fmt"
	"syscall"
	"unsafe"
)

// user32.dll input injection. SendInput and the INPUT structure are not
// part of golang.org/x/sys/windows, so they are declared here.
var (
	user32DLL     = syscall.NewLazyDLL("user32.dll")
	sendInputProc = user32DLL.NewProc("SendInput")

	kernel32DLL        = syscall.NewLazyDLL("kernel32.dll")
	process32FirstProc = kernel32DLL.NewProc("Process32FirstW")
	process32NextProc  = kernel32DLL.NewProc("Process32NextW")

	// GetDC/ReleaseDC are user32 functions, not gdi32 — looking them up
	// in gdi32 fails at first capture and panics the process.
	gdiGetDCProc     = user32DLL.NewProc("GetDC")
	gdiReleaseDCProc = user32DLL.NewProc("ReleaseDC")

	gdi32DLL                  = syscall.NewLazyDLL("gdi32.dll")
	gdiCreateCompatibleDCProc = gdi32DLL.NewProc("CreateCompatibleDC")
	gdiDeleteDCProc           = gdi32DLL.NewProc("DeleteDC")
	gdiCreateCompatBmpProc    = gdi32DLL.NewProc("CreateCompatibleBitmap")
	gdiDeleteObjectProc       = gdi32DLL.NewProc("DeleteObject")
	gdiSelectObjectProc       = gdi32DLL.NewProc("SelectObject")
	gdiBitBltProc             = gdi32DLL.NewProc("BitBlt")
)

// gdiSrcCopy is the Win32 SRCCOPY raster-operation.
const gdiSrcCopy = 0x00CC0020

// processEntry32W mirrors Win32 PROCESSENTRY32W (tlhelp32.h).
type processEntry32W struct {
	DwSize              uint32
	CntUsage            uint32
	Th32ProcessID       uint32
	_                   uint32
	Th32DefaultHeapID   uintptr
	Th32ModuleID        uint32
	CntThreads          uint32
	Th32ParentProcessID uint32
	PcPriClassBase      int32
	Th32SessionID       uint32
	DwFlags             uint32
	SzExeFile           [260]uint16
}

// input mirrors the Win32 INPUT structure (input.h). The mouse,
// keyboard and hardware union members share the 28-byte union storage;
// mi()/ki() alias it with the appropriate type.
type input struct {
	Type  uint32
	_     [4]byte
	union [28]byte
}

// mouseInput mirrors Win32 MOUSEINPUT.
type mouseInput struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

// keyboardInput mirrors Win32 KEYBDINPUT.
type keyboardInput struct {
	WvVk        uint16
	WvScanCode  uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

func (in *input) mi() *mouseInput {
	return (*mouseInput)(unsafe.Pointer(&in.union))
}

func (in *input) ki() *keyboardInput {
	return (*keyboardInput)(unsafe.Pointer(&in.union))
}

// sendInputOne injects a single input event.
func sendInputOne(in *input) error {
	n, _, errno := sendInputProc.Call(
		1,
		uintptr(unsafe.Pointer(in)),
		unsafe.Sizeof(*in),
	)
	if n != 1 {
		return fmt.Errorf("SendInput: %w", errno)
	}
	return nil
}

const (
	inputMouse    = 0
	inputKeyboard = 1

	keyeventfKeyup = 0x0002

	mouseeventfMove       = 0x0001
	mouseeventfLeftdown   = 0x0002
	mouseeventfLeftup     = 0x0004
	mouseeventfRightdown  = 0x0008
	mouseeventfRightup    = 0x0010
	mouseeventfMiddledown = 0x0020
	mouseeventfMiddleup   = 0x0040
	mouseeventfWheel      = 0x0800
	mouseeventfAbsolute   = 0x8000
)
