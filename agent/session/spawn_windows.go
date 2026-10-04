//go:build windows

package session

import (
	"fmt"
	"log"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// findProcessInSession returns a PID running in the given Windows
// session, preferring explorer.exe (any process works: it only needs
// a token we can duplicate).
func findProcessInSession(sessionID uint32) (int, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snap)

	var pe processEntry32W
	pe.DwSize = uint32(unsafe.Sizeof(pe))

	fallback := 0
	scanned := 0
	r1, _, e1 := process32FirstProc.Call(uintptr(snap), uintptr(unsafe.Pointer(&pe)))
	if r1 == 0 {
		return 0, fmt.Errorf("Process32FirstW: %v", e1)
	}
	for r1 != 0 {
		scanned++
		if pe.Th32SessionID == sessionID && fallback == 0 {
			fallback = int(pe.Th32ProcessID)
		}
		if pe.Th32SessionID == sessionID && windows.UTF16ToString(pe.SzExeFile[:]) == "explorer.exe" {
			return int(pe.Th32ProcessID), nil
		}
		pe.DwSize = uint32(unsafe.Sizeof(pe))
		r1, _, _ = process32NextProc.Call(uintptr(snap), uintptr(unsafe.Pointer(&pe)))
	}
	if fallback != 0 {
		return fallback, nil
	}
	return 0, fmt.Errorf("no process found in session %d (scanned %d processes)", sessionID, scanned)
}

// createProcessAsUserProc calls kernel32!CreateProcessAsUserW directly:
// x/sys/windows.StartupInfo omits hStdError, which we need to capture
// the helper's stderr. The export name carries the W suffix — neither
// advapi32 nor kernel32 exports an undecorated "CreateProcessAsUser",
// so the previous bindings failed at lookup and panicked the process.
var createProcessAsUserProc = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateProcessAsUserW")

// fullStartupInfo mirrors kernel32's STARTUPINFOW, including hStdError.
type fullStartupInfo struct {
	Cb            uint32
	Reserved      *uint16
	Desktop       *uint16
	Title         *uint16
	X             uint32
	Y             uint32
	XSize         uint32
	YSize         uint32
	XCountChars   uint32
	YCountChars   uint32
	FillAttribute uint32
	Flags         uint32
	ShowWindow    uint16
	CbReserved2   uint16
	Reserved2     *byte
	StdInput      windows.Handle
	StdOutput     windows.Handle
	StdError      windows.Handle
}

// wtsSessionInfoW mirrors Win32 WTS_SESSION_INFOW (wtsapi32.h).
type wtsSessionInfoW struct {
	SessionID      uint32
	WinStationName *uint16
	State          uint32
}

var (
	wtsapiDLL            = syscall.NewLazyDLL("wtsapi32.dll")
	wtsEnumerateSessions = wtsapiDLL.NewProc("WTSEnumerateSessionsW")
	wtsFreeMemory        = wtsapiDLL.NewProc("WTSFreeMemory")
)

// activeUserSessionIDs returns candidate session IDs that may hold a
// logged-on user: every WTSActive session (console or RDP, excluding
// services session 0), then disconnected sessions, then the console
// session. WTSGetActiveConsoleSessionId alone is not enough: when the
// machine is administered over RDP the console session has no user and
// the RDP session is the live one.
func activeUserSessionIDs() []uint32 {
	var ids []uint32
	var info *wtsSessionInfoW
	var count uint32
	const wtsActive, wtsDisconnected = 1, 4
	r1, _, _ := wtsEnumerateSessions.Call(0, 0, 1, uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&count)))
	if r1 != 0 && info != nil {
		defer wtsFreeMemory.Call(uintptr(unsafe.Pointer(info)))
		slice := unsafe.Slice(info, count)
		var disconnected []uint32
		for i := range slice {
			switch slice[i].State {
			case wtsActive:
				if slice[i].SessionID != 0 {
					ids = append(ids, slice[i].SessionID)
				}
			case wtsDisconnected:
				// Session 0 ("Services") is listed as disconnected on
				// modern Windows; it never holds a desktop or a user.
				if slice[i].SessionID != 0 {
					disconnected = append(disconnected, slice[i].SessionID)
				}
			}
		}
		ids = append(ids, disconnected...)
	}
	if console := windows.WTSGetActiveConsoleSessionId(); console != 0 && console != ^uint32(0) {
		ids = append(ids, console)
	}
	return ids
}

// userSessionToken finds a user token for the first session that holds
// a logged-on user (see activeUserSessionIDs). Exported indirectly via
// spawnRemote: the remote-control executable needs the same token
// discovery but a different process.
func userSessionToken() (windows.Token, error) {
	var lastErr error
	for _, candidate := range activeUserSessionIDs() {
		var token windows.Token
		if err := windows.WTSQueryUserToken(candidate, &token); err != nil {
			pid, ferr := findProcessInSession(candidate)
			if ferr != nil {
				lastErr = fmt.Errorf("session %d: no logged-on user (WTSQueryUserToken: %v; %w)", candidate, err, ferr)
				continue
			}
			hProc, oerr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
			if oerr != nil {
				lastErr = fmt.Errorf("session %d: open process %d: %w", candidate, pid, oerr)
				continue
			}
			if err := windows.OpenProcessToken(hProc, windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE, &token); err != nil {
				windows.CloseHandle(hProc)
				lastErr = fmt.Errorf("session %d: open process token: %w", candidate, err)
				continue
			}
			var dup windows.Token
			if err := windows.DuplicateTokenEx(token, windows.MAXIMUM_ALLOWED, nil, windows.SecurityImpersonation, windows.TokenPrimary, &dup); err != nil {
				windows.CloseHandle(hProc)
				windows.CloseHandle(windows.Handle(token))
				lastErr = fmt.Errorf("session %d: duplicate token: %w", candidate, err)
				continue
			}
			windows.CloseHandle(hProc)
			windows.CloseHandle(windows.Handle(token))
			return dup, nil
		}
		return token, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no active user session")
	}
	return windows.Token(0), lastErr
}

// systemSessionToken returns a primary token for the agent's own
// identity (LocalSystem when running as the service) retargeted to the
// first interactive session, or an error when the agent is not running
// as a Session 0 service or no session exists.
func systemSessionToken() (windows.Token, error) {
	if !runningInSession0() {
		return 0, fmt.Errorf("agent is not running as a service")
	}
	ids := activeUserSessionIDs()
	if len(ids) == 0 {
		return 0, fmt.Errorf("no interactive session")
	}
	var self windows.Token
	const want = windows.TOKEN_DUPLICATE | windows.TOKEN_QUERY | windows.TOKEN_ASSIGN_PRIMARY |
		windows.TOKEN_ADJUST_DEFAULT | windows.TOKEN_ADJUST_SESSIONID
	if err := windows.OpenProcessToken(windows.CurrentProcess(), want, &self); err != nil {
		return 0, fmt.Errorf("open own token: %w", err)
	}
	defer windows.CloseHandle(windows.Handle(self))
	var dup windows.Token
	if err := windows.DuplicateTokenEx(self, windows.MAXIMUM_ALLOWED, nil, windows.SecurityImpersonation, windows.TokenPrimary, &dup); err != nil {
		return 0, fmt.Errorf("duplicate own token: %w", err)
	}
	sid := ids[0]
	if sid == 0 {
		return 0, fmt.Errorf("refusing to target Session 0")
	}
	if err := windows.SetTokenInformation(dup, windows.TokenSessionId, (*byte)(unsafe.Pointer(&sid)), uint32(unsafe.Sizeof(sid))); err != nil {
		windows.CloseHandle(windows.Handle(dup))
		return 0, fmt.Errorf("set token session %d: %w", sid, err)
	}
	return dup, nil
}

// SpawnRemote launches the per-session remote-control executable in
// the interactive user session with its window suppressed (the binary
// is GUI-subsystem; CREATE_NO_WINDOW keeps any stray console hidden).
// The agent supervises the returned PID and kills it when the session
// ends.
func SpawnRemote(exe, cmdLine string) (int, error) {
	// Prefer a SYSTEM token placed in the user's session: that process
	// can attach to the Winlogon desktop, so UAC prompts and the
	// logon/lock screen are visible and controllable. Fall back to the
	// logged-on user's token (normal desktop only).
	token, err := systemSessionToken()
	if err != nil {
		log.Printf("session: SYSTEM remote token unavailable (%v); using the user token", err)
		token, err = userSessionToken()
		if err != nil {
			return 0, err
		}
	}
	defer windows.CloseHandle(windows.Handle(token))

	var envBlock *uint16
	if err := windows.CreateEnvironmentBlock(&envBlock, token, false); err != nil {
		return 0, fmt.Errorf("create environment block: %w", err)
	}
	defer windows.DestroyEnvironmentBlock(envBlock)

	var si fullStartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	desktop, _ := windows.UTF16PtrFromString(`winsta0\default`)
	si.Desktop = desktop
	exe16, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	cmd16, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		return 0, err
	}
	const createNoWindow = 0x08000000
	var pi windows.ProcessInformation
	r, _, cpaErr := createProcessAsUserProc.Call(
		uintptr(token),
		uintptr(unsafe.Pointer(exe16)),
		uintptr(unsafe.Pointer(cmd16)),
		0, 0,
		1,
		uintptr(windows.CREATE_UNICODE_ENVIRONMENT|createNoWindow),
		uintptr(unsafe.Pointer(envBlock)),
		0,
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if r == 0 {
		return 0, fmt.Errorf("CreateProcessAsUser: %w", cpaErr)
	}
	windows.CloseHandle(pi.Process)
	windows.CloseHandle(pi.Thread)
	return int(pi.ProcessId), nil
}
