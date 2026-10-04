//go:build windows

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// crashLogPath is where native crashes are recorded (the agent and the
// remote exe run without a console).
const crashLogPath = `C:\ProgramData\OurWay\crash.log`

// A native crash (an access violation inside a Win32 call) kills the
// process with no output: the service has no stderr, and the Go runtime
// only reports faults it raised itself. This installs a process-wide
// unhandled-exception filter that appends the exception code and
// faulting address to crashLogPath before the process dies, so a silent
// death is diagnosable.

// exceptionRecord mirrors the EXCEPTION_RECORD fields we use (x64
// layout).
type exceptionRecord struct {
	ExceptionCode        uint32
	ExceptionFlags       uint32
	ExceptionRecord      unsafe.Pointer
	ExceptionAddress     unsafe.Pointer
	NumberParameters     uint32
	_                    [2]uint32
	ExceptionInformation [15]uintptr
}

// exceptionPointers mirrors EXCEPTION_POINTERS.
type exceptionPointers struct {
	ExceptionRecord *exceptionRecord
	ContextRecord   unsafe.Pointer
}

var setExceptionFilterProc = syscall.NewLazyDLL("kernel32.dll").NewProc("SetUnhandledExceptionFilter")

// InstallCrashFilter records native crashes to crashLogPath. Call once
// per process at startup; safe in both the service and the helper.
func InstallCrashFilter() {
	setExceptionFilterProc.Call(syscall.NewCallback(exceptionFilterCallback))
}

// exceptionFilterCallback runs on the crashing thread. It must be a plain
// function (no closure) for syscall.NewCallback, and it should stay
// allocation-light. Returning 0 (EXCEPTION_CONTINUE_EXECUTION) lets the
// default handler run — the process still dies, we only observed it.
func exceptionFilterCallback(ep unsafe.Pointer) uintptr {
	p := (*exceptionPointers)(ep)
	var code uint32
	var addr uintptr
	if p != nil && p.ExceptionRecord != nil {
		code = p.ExceptionRecord.ExceptionCode
		addr = uintptr(p.ExceptionRecord.ExceptionAddress)
	}
	os.MkdirAll(filepath.Dir(crashLogPath), 0o755)
	if f, err := os.OpenFile(crashLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintf(f, "=== CRASH exception=0x%08x faulting_addr=0x%x ===\n", code, addr)
		f.Close()
	}
	return 0
}
