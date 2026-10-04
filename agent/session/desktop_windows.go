//go:build windows

package session

import (
	"log"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// Following the input desktop: UAC prompts, Ctrl+Alt+Del and the
// logon/lock screen live on the Winlogon desktop, not "Default". A
// thread can only see and drive the desktop it is attached to, so the
// capture and input threads re-attach to the current input desktop
// (OpenInputDesktop + SetThreadDesktop) before each frame / event.
// Opening the Winlogon desktop needs SYSTEM; as an ordinary user
// OpenInputDesktop simply fails and the thread stays where it is.

var (
	procOpenInputDesktop          = user32DLL.NewProc("OpenInputDesktop")
	procSetThreadDesktop          = user32DLL.NewProc("SetThreadDesktop")
	procCloseDesktop              = user32DLL.NewProc("CloseDesktop")
	procGetUserObjectInformationW = user32DLL.NewProc("GetUserObjectInformationW")
)

const (
	genericAll = 0x10000000
	uoiName    = 2
)

// threadDesktop tracks the desktop one OS thread is attached to. It
// must only be used from a single, locked OS thread.
type threadDesktop struct {
	handle uintptr
	name   string
	warned bool
}

// attach switches the calling thread to the current input desktop. It
// returns the desktop name and whether it changed since the last call.
func (t *threadDesktop) attach() (name string, changed bool) {
	h, _, _ := procOpenInputDesktop.Call(0, 0, genericAll)
	if h == 0 {
		// Mid-switch or not permitted: keep the current desktop.
		return t.name, false
	}
	n := desktopName(h)
	if t.handle != 0 && n == t.name {
		procCloseDesktop.Call(h)
		return t.name, false
	}
	if r, _, err := procSetThreadDesktop.Call(h); r == 0 {
		procCloseDesktop.Call(h)
		if !t.warned {
			t.warned = true
			log.Printf("session: SetThreadDesktop(%q) failed: %v", n, err)
		}
		return t.name, false
	}
	if t.handle != 0 {
		procCloseDesktop.Call(t.handle)
	}
	t.handle, t.name = h, n
	log.Printf("session: attached to desktop %q", n)
	return n, true
}

func desktopName(h uintptr) string {
	var buf [256]uint16
	var need uint32
	r, _, _ := procGetUserObjectInformationW.Call(h, uoiName,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}

// isDefaultDesktop reports whether name is the normal user desktop.
func isDefaultDesktop(name string) bool {
	return strings.EqualFold(name, "Default")
}

// inputWorker runs input injection on one dedicated, locked OS thread
// that follows the input desktop.
type inputWorker struct {
	once sync.Once
	jobs chan inputJob
}

type inputJob struct {
	fn   func() error
	done chan error
}

var injector inputWorker

func (w *inputWorker) run(fn func() error) error {
	w.once.Do(func() {
		w.jobs = make(chan inputJob)
		go func() {
			runtime.LockOSThread()
			var td threadDesktop
			for j := range w.jobs {
				td.attach()
				j.done <- j.fn()
			}
		}()
	})
	done := make(chan error, 1)
	w.jobs <- inputJob{fn: fn, done: done}
	return <-done
}
