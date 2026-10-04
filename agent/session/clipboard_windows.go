//go:build windows

package session

import (
	"fmt"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	procOpenClipboard      = user32DLL.NewProc("OpenClipboard")
	procCloseClipboard     = user32DLL.NewProc("CloseClipboard")
	procEmptyClipboard     = user32DLL.NewProc("EmptyClipboard")
	procGetClipboardData   = user32DLL.NewProc("GetClipboardData")
	procSetClipboardData   = user32DLL.NewProc("SetClipboardData")
	procClipboardSeqNumber = user32DLL.NewProc("GetClipboardSequenceNumber")

	procGlobalAlloc  = kernel32DLL.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32DLL.NewProc("GlobalFree")
	procGlobalLock   = kernel32DLL.NewProc("GlobalLock")
	procGlobalUnlock = kernel32DLL.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32DLL.NewProc("GlobalSize")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// winClipboard is the text clipboard of the current window station.
type winClipboard struct{}

// NewClipboard returns the interactive session's text clipboard.
func NewClipboard() Clipboard { return winClipboard{} }

func (winClipboard) Seq() uint32 {
	n, _, _ := procClipboardSeqNumber.Call()
	return uint32(n)
}

// open retries briefly: another process may hold the clipboard.
func openClipboard() error {
	var err error
	for i := 0; i < 10; i++ {
		var r uintptr
		r, _, err = procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("OpenClipboard: %v", err)
}

func (winClipboard) Get() (string, error) {
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil // no text on the clipboard
	}
	size, _, _ := procGlobalSize.Call(h)
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", fmt.Errorf("GlobalLock failed")
	}
	defer procGlobalUnlock.Call(h)
	n := int(size) / 2
	units := unsafe.Slice((*uint16)(globalPtr(p)), n)
	for i, u := range units {
		if u == 0 {
			n = i
			break
		}
	}
	return string(utf16.Decode(units[:n])), nil
}

func (winClipboard) Set(text string) error {
	u := utf16.Encode([]rune(text))
	u = append(u, 0)
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %v", err)
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock failed")
	}
	copy(unsafe.Slice((*uint16)(globalPtr(p)), len(u)), u)
	procGlobalUnlock.Call(h)

	if err := openClipboard(); err != nil {
		procGlobalFree.Call(h)
		return err
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData: %v", err)
	}
	return nil // the system owns h now
}

// globalPtr converts the address GlobalLock returned (memory outside
// the Go heap) to an unsafe.Pointer without tripping vet's uintptr check.
func globalPtr(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }
