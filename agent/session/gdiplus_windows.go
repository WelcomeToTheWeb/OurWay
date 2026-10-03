//go:build windows

package session

import (
	"fmt"
	"syscall"
	"unsafe"
)

// GDI+ (gdiplus.dll) provides the system JPEG encoder. All GDI+ calls in
// the agent go through this file. GDI+ is per-thread: the capture
// goroutine must runtime.LockOSThread() and call gdiplusEnsureStarted()
// before encoding (see screen_capture_windows.go).
var (
	gdiplusDLL         = syscall.NewLazyDLL("gdiplus.dll")
	gdiplusStartupProc = gdiplusDLL.NewProc("GdiplusStartup")
	gdiplusCreateBmp   = gdiplusDLL.NewProc("GdipCreateBitmapFromHBITMAP")
	gdiplusSaveFile    = gdiplusDLL.NewProc("GdipSaveImageToFile")
	gdiplusDispose     = gdiplusDLL.NewProc("GdipDisposeImage")
)

// gdiplusStartupInput mirrors GdiplusStartupInput (gdiplus.h).
type gdiplusStartupInput struct {
	GdiplusVersion           uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread uint32
	SuppressExternalCodecs   uint32
}

// gdiplusEncoderParameter mirrors GdiplusImageProperty (gdiplus.h):
// {GUID Guid; ULONG NumberOfValues; ULONG Type; VOID* Value}.
type gdiplusEncoderParameter struct {
	Guid  syscall.GUID
	Count uint32
	Type  uint32
	Value unsafe.Pointer
}

// gdiplusEncoderParameters mirrors GdiplusImaging.h's EncoderParameters:
// {UINT Count; EncoderParameter Parameter[1]}. GdipSaveImageToFile takes
// THIS type, not a bare EncoderParameter — passing the latter makes GDI+
// read the first bytes of the quality GUID as the parameter count and walk
// out of bounds (a native access violation that kills the process).
type gdiplusEncoderParameters struct {
	Count     uint32
	_         uint32
	Parameter [1]gdiplusEncoderParameter
}

const (
	gdiplusStatusOK     = 0
	gdiplusPropertyLong = 4
)

// encoderQualityGUID is GdiplusImaging.h's EncoderQuality GUID
// {1D5BE4B5-FA4A-452D-9CDD-5DB35105E7EB}.
var encoderQualityGUID = syscall.GUID{
	Data1: 0x1d5be4b5,
	Data2: 0xfa4a,
	Data3: 0x452d,
	Data4: [8]byte{0x9c, 0xdd, 0x5d, 0xb3, 0x51, 0x05, 0xe7, 0xeb},
}

// jpegEncoderGUID is the built-in JPEG image encoder CLSID
// {557CF401-1A04-11D3-9A73-0000F81EF32}.
var jpegEncoderGUID = syscall.GUID{
	Data1: 0x557cf401,
	Data2: 0x1a04,
	Data3: 0x11d3,
	Data4: [8]byte{0x9a, 0x73, 0x00, 0x00, 0xf8, 0x1e, 0xf3, 0x2},
}


var gdiplusToken uintptr

// gdiplusEnsureStarted starts GDI+ on the current (locked) thread.
func gdiplusEnsureStarted() error {
	if gdiplusToken != 0 {
		return nil
	}
	var in gdiplusStartupInput
	in.GdiplusVersion = 1
	r1, _, _ := gdiplusStartupProc.Call(
		uintptr(unsafe.Pointer(&gdiplusToken)),
		uintptr(unsafe.Pointer(&in)),
		0,
	)
	if r1 != gdiplusStatusOK {
		return fmt.Errorf("GdiplusStartup failed: %d", r1)
	}
	return nil
}

// jpegEncoderCLSID returns the GDI+ JPEG encoder CLSID. It is a fixed,
// documented constant ({557CF401-1A04-11D3-9A73-0000F81EF32}, ImageFormatJPEG
// encoder per GdiplusImaging.h) stable across Windows versions, so it is
// hardcoded rather than discovered by walking the codec array — the
// size/stride iteration was fragile and failed on some systems.
func jpegEncoderCLSID() (*syscall.GUID, error) {
	guid := jpegEncoderGUID
	return &guid, nil
}

// gdiplusSaveJPEG encodes hBmp to path as JPEG at the given quality
// (1-100). Must run on a thread that called gdiplusEnsureStarted().
func gdiplusSaveJPEG(hBmp uintptr, path string, quality int32) error {
	var bitmap uintptr
	r1, _, _ := gdiplusCreateBmp.Call(hBmp, 0, uintptr(unsafe.Pointer(&bitmap)))
	if r1 != gdiplusStatusOK {
		return fmt.Errorf("GdipCreateBitmapFromHBITMAP failed: %d", r1)
	}
	defer gdiplusDispose.Call(bitmap)

	clsid, err := jpegEncoderCLSID()
	if err != nil {
		return err
	}

	var qualityVal int32 = quality
	var params gdiplusEncoderParameters
	params.Count = 1
	params.Parameter[0] = gdiplusEncoderParameter{
		Guid:  encoderQualityGUID,
		Count: 1,
		Type:  gdiplusPropertyLong,
		Value: unsafe.Pointer(&qualityVal),
	}

	path16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r1, _, _ = gdiplusSaveFile.Call(
		bitmap,
		uintptr(unsafe.Pointer(path16)),
		uintptr(unsafe.Pointer(clsid)),
		uintptr(unsafe.Pointer(&params)),
	)
	if r1 != gdiplusStatusOK {
		return fmt.Errorf("GdipSaveImageToFile failed: %d", r1)
	}
	return nil
}
