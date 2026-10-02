//go:build windows

package session

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// helperLogPath is where the spawned helper's stdout/stderr (Go log
// output and panic traces) is appended; the helper runs without a
// console, so without this its failures are invisible.
const helperLogPath = `C:\ProgramData\OurWay\user-helper.log`

// userHelper is the service-side handle to a spawned per-user helper
// process. The helper runs this same binary with --user-helper in the
// active console session and streams JPEG frames back over loopback
// TCP while relaying input events via SendInput.
type userHelper struct {
	mu     sync.Mutex
	conn   net.Conn
	w      *bufio.Writer
	proc   *os.Process
	ready  chan struct{}
	closed chan struct{}
}

func (h *userHelper) send(v map[string]interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return fmt.Errorf("helper connection closed")
	}
	if _, err := h.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return h.w.Flush()
}

func (h *userHelper) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return
	}
	h.conn.Close()
	h.conn = nil
	if h.proc != nil {
		h.proc.Kill()
	}
}

// helperExitCode waits briefly for the helper to exit and returns its
// exit code: 2 = Go panic, 0xC0000005 = access violation in native
// code, 0xFFFFFFFF = terminated by us.
func helperExitCode(pid int) (uint32, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(h)
	var code uint32
	for range 20 {
		if err := windows.GetExitCodeProcess(h, &code); err != nil {
			return 0, false
		}
		if code != 259 { // STILL_ACTIVE
			return code, true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return code, true
}

// readLoop consumes helper messages until the connection drops. Frame
// data is stashed on the capture so Capture() can serve it.
func (h *userHelper) readLoop(c *windowsCapture) {
	defer func() {
		close(h.closed)
		h.close()
		extra := ""
		if h.proc != nil {
			if code, ok := helperExitCode(h.proc.Pid); ok {
				extra = fmt.Sprintf(" (helper exit code %#x)", code)
			}
		}
		log.Printf("session: per-user helper connection closed%s; helper output: %s", extra, helperLogPath)
		c.mu.Lock()
		if c.helper == h {
			c.helper = nil
		}
		c.mu.Unlock()
	}()

	r := bufio.NewReader(h.conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var msg struct {
			Type    string `json:"type"`
			Data    string `json:"data"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "ready":
			close(h.ready)
		case "frame":
			data, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil || len(data) == 0 {
				continue
			}
			c.lastFrame.Store(data)
			c.lastAt.Store(time.Now().UnixNano())
		case "error":
			log.Printf("session: helper error: %s", msg.Message)
		}
	}
}

// spawnHelperLocked starts the helper as the active console session
// user and waits for its ready message. Caller holds c.mu.
func (c *windowsCapture) spawnHelperLocked() (*userHelper, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	exe, err := os.Executable()
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("executable path: %w", err)
	}

	pid, err := createProcessAsUser(exe, fmt.Sprintf(`"%s" --user-helper 127.0.0.1:%d`, exe, port))
	if err != nil {
		ln.Close()
		return nil, fmt.Errorf("spawn: %w", err)
	}

	type acceptResult struct {
		conn net.Conn
		err  error
	}
	accepted := make(chan acceptResult, 1)
	go func() {
		conn, err := ln.Accept()
		accepted <- acceptResult{conn, err}
	}()

	select {
	case r := <-accepted:
		ln.Close()
		if r.err != nil {
			killPID(pid)
			return nil, fmt.Errorf("accept: %w", r.err)
		}
		h := &userHelper{
			conn:   r.conn,
			w:      bufio.NewWriter(r.conn),
			proc:   mustFindProcess(pid),
			ready:  make(chan struct{}),
			closed: make(chan struct{}),
		}
		go h.readLoop(c)
		select {
		case <-h.ready:
			return h, nil
		case <-time.After(10 * time.Second):
			h.close()
			return nil, fmt.Errorf("helper did not report ready")
		}
	case <-time.After(10 * time.Second):
		ln.Close()
		killPID(pid)
		return nil, fmt.Errorf("helper did not connect")
	}
}

// killPID terminates a process by PID (best effort).
func killPID(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}

func mustFindProcess(pid int) *os.Process {
	p, _ := os.FindProcess(pid)
	return p
}

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

// createProcessAsUserProc calls advapi32!CreateProcessAsUser directly:
// x/sys/windows.StartupInfo omits hStdError, which we need to capture
// the helper's stderr.
var createProcessAsUserProc = syscall.NewLazyDLL("advapi32.dll").NewProc("CreateProcessAsUser")

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

// createProcessAsUser launches exe with the given command line as the
// user of the active console session, on the interactive desktop.
func createProcessAsUser(exe, cmdLine string) (int, error) {
	sessionID := windows.WTSGetActiveConsoleSessionId()
	if sessionID == 0 || sessionID == ^uint32(0) {
		return 0, fmt.Errorf("no active console session")
	}

	// Primary path: WTSQueryUserToken returns the primary token of the
	// user logged on to the session directly — no process enumeration.
	// The service runs as LocalSystem, which has SE_TCB.
	var token windows.Token
	tokenSource := "WTSQueryUserToken"
	if err := windows.WTSQueryUserToken(sessionID, &token); err != nil {
		// Fallback: duplicate the token of a process in the session.
		pid, ferr := findProcessInSession(sessionID)
		if ferr != nil {
			return 0, fmt.Errorf("no logged-on user in session %d (WTSQueryUserToken: %v; %w)", sessionID, err, ferr)
		}
		tokenSource = fmt.Sprintf("process %d", pid)
		hProc, oerr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if oerr != nil {
			return 0, fmt.Errorf("open process %d: %w", pid, oerr)
		}
		defer windows.CloseHandle(hProc)
		if err := windows.OpenProcessToken(hProc, windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE, &token); err != nil {
			return 0, fmt.Errorf("open process token: %w", err)
		}
		var dup windows.Token
		if err := windows.DuplicateTokenEx(token, windows.MAXIMUM_ALLOWED, nil, windows.SecurityImpersonation, windows.TokenPrimary, &dup); err != nil {
			return 0, fmt.Errorf("duplicate token: %w", err)
		}
		windows.CloseHandle(windows.Handle(token))
		token = dup
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

	// The helper has no console: capture its stdout/stderr (Go log
	// output and panic traces) in a file so failures are diagnosable.
	var logHandle windows.Handle
	if f, ferr := os.OpenFile(helperLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); ferr == nil {
		fmt.Fprintf(f, "\n=== user-helper spawned %s ===\n", time.Now().Format(time.RFC3339))
		logHandle = windows.Handle(f.Fd())
		defer f.Close()
	}
	if logHandle != 0 {
		si.StdOutput = logHandle
		si.StdError = logHandle
	}

	log.Printf("session: CreateProcessAsUser: about to spawn (token from %s, stdHandle=%#x)", tokenSource, logHandle)

	var pi windows.ProcessInformation
	r, _, cpaErr := createProcessAsUserProc.Call(
		uintptr(token),
		uintptr(unsafe.Pointer(exe16)),
		uintptr(unsafe.Pointer(cmd16)),
		0, 0,
		1,
		uintptr(windows.CREATE_UNICODE_ENVIRONMENT),
		uintptr(unsafe.Pointer(envBlock)),
		0,
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	log.Printf("session: CreateProcessAsUser: returned r=%#x err=%v", r, cpaErr)
	if r == 0 {
		return 0, fmt.Errorf("CreateProcessAsUser (token from %s): %w", tokenSource, cpaErr)
	}
	windows.CloseHandle(pi.Process)
	windows.CloseHandle(pi.Thread)
	return int(pi.ProcessId), nil
}

// RunUserHelper runs this binary as the per-user capture/input helper.
// It dials the service at addr (127.0.0.1:port), announces ready, then
// serves frames at ~15fps while relaying input events until told to
// stop. It returns the process exit code.
func RunUserHelper(addr string) int {
	InstallCrashFilter()
	var conn net.Conn
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.Dial("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		log.Printf("user-helper: cannot reach service at %s: %v", addr, err)
		return 1
	}
	defer conn.Close()

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	send := func(v map[string]interface{}) {
		b, merr := json.Marshal(v)
		if merr != nil {
			return
		}
		w.Write(append(b, '\n'))
		w.Flush()
	}

	var quality atomic.Int32
	quality.Store(80)
	var running atomic.Bool

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if p := recover(); p != nil {
				send(map[string]interface{}{
					"type":    "error",
					"message": fmt.Sprintf("capture goroutine panic: %v\n%s", p, debug.Stack()),
				})
			}
		}()
		// GDI+ is per-thread; pin this goroutine to one OS thread and
		// start GDI+ exactly once there.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if serr := gdiplusEnsureStarted(); serr != nil {
			send(map[string]interface{}{"type": "error", "message": serr.Error()})
			return
		}
		tick := time.NewTicker(helperFrameInterval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				if !running.Load() {
					continue
				}
				data, cerr := gdiCaptureJPEG(int(quality.Load()))
				if cerr != nil {
					send(map[string]interface{}{"type": "error", "message": cerr.Error()})
					continue
				}
				send(map[string]interface{}{
					"type": "frame",
					"data": base64.StdEncoding.EncodeToString(data),
				})
			}
		}
	}()

	send(map[string]interface{}{"type": "ready"})

	for {
		line, rerr := r.ReadBytes('\n')
		if rerr != nil {
			return 0
		}
		var msg struct {
			Type    string                 `json:"type"`
			Quality int                    `json:"quality"`
			Input   map[string]interface{} `json:"input"`
		}
		if jerr := json.Unmarshal(line, &msg); jerr != nil {
			continue
		}
		switch msg.Type {
		case "start":
			running.Store(true)
			send(map[string]interface{}{"type": "start_ack"})
		case "quality":
			if msg.Quality >= 10 && msg.Quality <= 100 {
				quality.Store(int32(msg.Quality))
			}
		case "input":
			handleHelperInput(msg.Input)
		case "stop":
			running.Store(false)
			close(stop)
			wg.Wait()
			return 0
		}
	}
}

// handleHelperInput applies an input event in the helper's (user)
// session via SendInput.
func handleHelperInput(input map[string]interface{}) {
	typ, _ := input["type"].(string)
	switch typ {
	case "key":
		key, _ := input["key"].(string)
		event, _ := input["event"].(string)
		synthesizeKey(key, event)
	case "mouse":
		event, _ := input["event"].(string)
		x, _ := input["x"].(float64)
		y, _ := input["y"].(float64)
		button, _ := input["button"].(string)
		delta, _ := input["delta"].(float64)
		synthesizeMouse(event, x, y, button, delta)
	default:
		log.Printf("user-helper: unknown input type %q", typ)
	}
}
