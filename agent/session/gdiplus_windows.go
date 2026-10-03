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
	gdiplusEncodersSz  = gdiplusDLL.NewProc("GdipGetImageEncodersSize")
	gdiplusEncoders    = gdiplusDLL.NewProc("GdipGetImageEncoders")
	gdiplusDispose     = gdiplusDLL.NewProc("GdipDisposeImage")
)

// gdiplusStartupInput mirrors GdiplusStartupInput (gdiplus.h).
type gdiplusStartupInput struct {
	GdiplusVersion           uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread uint32
	SuppressExternalCodecs   uint32
}

// gdiplusImageCodec mirrors GdiplusImageCodecInfo (gdiplus.h). Only the
// offsets up to MimeType matter; iteration uses the stride reported by
// GdipGetImageEncodersSize.
type gdiplusImageCodec struct {
	ClassID     syscall.GUID
	FormatID    syscall.GUID
	MimeType    [64]uint16
	FileNameExt [80]uint16
	Flags1      uint32
	Flags2      uint32
	EncodingMin uint16
	EncodingMax uint16
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

// jpegEncoderCLSID returns a copy of the GDI+ JPEG encoder CLSID.
func jpegEncoderCLSID() (*syscall.GUID, error) {
	var count uint32
	var size uint32
	r1, _, _ := gdiplusEncodersSz.Call(
		uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 != gdiplusStatusOK || size == 0 || count == 0 {
		return nil, fmt.Errorf("GdipGetImageEncodersSize failed: %d", r1)
	}
	buf := make([]byte, size)
	r1, _, _ = gdiplusEncoders.Call(
		uintptr(count),
		uintptr(size),
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if r1 != gdiplusStatusOK {
		return nil, fmt.Errorf("GdipGetImageEncoders failed: %d", r1)
	}
	stride := int(size) / int(count)
	for i := range int(count) {
		enc := (*gdiplusImageCodec)(unsafe.Add(unsafe.Pointer(&buf[0]), i*stride))
		if syscall.UTF16ToString(enc.MimeType[:]) == "image/jpeg" {
			guid := enc.ClassID
			return &guid, nil
		}
	}
	return nil, fmt.Errorf("GDI+ JPEG encoder not found")
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
