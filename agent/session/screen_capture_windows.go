//go:build windows

package session

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

// helperFrameInterval is the capture cadence: ~15fps.
const helperFrameInterval = 70 * time.Millisecond

// windowsCapture captures the display (Desktop Duplication, falling
// back to GDI BitBlt) and encodes frames as JPEG. It must run in the
// interactive session: the agent service (Session 0) cannot see the
// desktop, so remote sessions run it inside ourway-remote.exe. Capture()
// serves the most recent frame from the internal 15fps producer.
type windowsCapture struct {
	quality atomic.Int32

	mu              sync.Mutex
	localLoopActive bool
	localStop       chan struct{}

	lastFrame atomic.Value // []byte
	lastAt    atomic.Int64 // unix nano

	// Native-viewer mode: the producer publishes raw frames of the
	// selected monitor instead of JPEGs.
	viewerMode atomic.Bool
	selMonitor atomic.Int32
	rawMu      sync.Mutex
	raw        RawFrame
	monCache   []Monitor
	monAt      time.Time
}

// NewScreenCapture creates the Windows screen capture.
func NewScreenCapture() ScreenCapture {
	c := &windowsCapture{localStop: make(chan struct{})}
	c.quality.Store(80)
	return c
}

// SetQuality sets the JPEG quality (0-100).
func (c *windowsCapture) SetQuality(quality int) {
	if quality < 10 {
		quality = 10
	}
	if quality > 100 {
		quality = 100
	}
	c.quality.Store(int32(quality))
}

// Capture returns the most recent JPEG frame, waiting briefly for a
// fresh one if needed.
func (c *windowsCapture) Capture() ([]byte, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if v := c.lastFrame.Load(); v != nil {
			if time.Since(time.Unix(0, c.lastAt.Load())) < 2*time.Second {
				return v.([]byte), nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("no recent frame from capture")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// startSession starts the frame producer. It refuses to run in
// Session 0, where there is no desktop to capture.
func (c *windowsCapture) startSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.localLoopActive {
		return nil
	}
	if runningInSession0() {
		return fmt.Errorf("cannot capture in Session 0: the remote-control exe is unavailable")
	}
	c.startLocalLoopLocked()
	return nil
}

// stopSession stops the frame producer.
func (c *windowsCapture) stopSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.localLoopActive {
		c.localLoopActive = false
		close(c.localStop)
		c.localStop = make(chan struct{})
	}
}

func (c *windowsCapture) startLocalLoopLocked() {
	if c.localLoopActive {
		return
	}
	c.localLoopActive = true
	stop := c.localStop
	go func() {
		// Capture runs on one locked OS thread that follows the input
		// desktop (UAC / logon screen), and owns all DXGI/GDI calls.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var td threadDesktop
		push := func() {
			if name, changed := td.attach(); changed {
				dxgiState.desktopChanged(name)
			}
			if c.viewerMode.Load() {
				c.pushRawFrame()
			} else {
				c.pushLocalFrame()
			}
		}
		// First frame immediately so the viewer does not sit on
		// "waiting for first frame".
		push()
		tick := time.NewTicker(helperFrameInterval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				push()
			}
		}
	}()
}

func (c *windowsCapture) pushLocalFrame() {
	quality := int(c.quality.Load())
	// Desktop Duplication first (GPU compositor, fast, sees everything);
	// it internally falls back to GDI when unavailable.
	data, err := dxgiState.Frame(quality)
	if err != nil {
		if err == errDXGIFrameStale {
			return
		}
		log.Printf("session: local capture failed: %v", err)
		return
	}
	c.lastFrame.Store(data)
	c.lastAt.Store(time.Now().UnixNano())
}

// SetViewerMode implements RawSource.
func (c *windowsCapture) SetViewerMode(on bool) {
	c.viewerMode.Store(on)
	if on {
		c.applyInputRegion()
	} else {
		setInputRegion(nil) // browser path: primary monitor mapping
	}
}

// Monitors implements RawSource.
func (c *windowsCapture) Monitors() []Monitor { return enumMonitors() }

// SelectMonitor implements RawSource.
func (c *windowsCapture) SelectMonitor(id int) error {
	if id != MonitorAll {
		mons := enumMonitors()
		if id < 0 || id >= len(mons) {
			return fmt.Errorf("no monitor %d", id)
		}
	}
	c.selMonitor.Store(int32(id))
	c.applyInputRegion()
	return nil
}

// selectedRect resolves the selected monitor to its rectangle,
// falling back to monitor 0 if the layout changed under it.
func (c *windowsCapture) selectedRect() (id int, r image.Rectangle) {
	id = int(c.selMonitor.Load())
	if id == MonitorAll {
		return id, virtualScreenRect()
	}
	c.rawMu.Lock()
	if time.Since(c.monAt) > time.Second || len(c.monCache) == 0 {
		c.monCache, c.monAt = enumMonitors(), time.Now()
	}
	mons := c.monCache
	c.rawMu.Unlock()
	if id < 0 || id >= len(mons) {
		id = 0
	}
	return id, mons[id].Rect()
}

// applyInputRegion points mouse input at the selected monitor.
func (c *windowsCapture) applyInputRegion() {
	_, r := c.selectedRect()
	setInputRegion(&r)
}

// NextRaw implements RawSource.
func (c *windowsCapture) NextRaw(after uint64, timeout time.Duration) (RawFrame, bool) {
	deadline := time.Now().Add(timeout)
	for {
		c.rawMu.Lock()
		f := c.raw
		c.rawMu.Unlock()
		if f.Img != nil && f.Seq > after {
			return f, true
		}
		if time.Now().After(deadline) {
			return RawFrame{}, false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// pushRawFrame grabs the selected monitor and publishes it when it
// changed. It runs on the capture thread.
func (c *windowsCapture) pushRawFrame() {
	id, rect := c.selectedRect()
	img, changed, ok := dxgiState.Raw(rect)
	if !ok {
		var err error
		if img, err = gdiCaptureRGBA(rect); err != nil {
			log.Printf("session: viewer capture failed: %v", err)
			return
		}
		changed = true
	}
	c.rawMu.Lock()
	defer c.rawMu.Unlock()
	// A monitor switch must publish even when the pixels are unchanged.
	if !changed && c.raw.Monitor == id && c.raw.Img != nil {
		return
	}
	c.raw = RawFrame{Img: img, Seq: c.raw.Seq + 1, Monitor: id}
}

// runningInSession0 reports whether this process runs in the Windows
// non-interactive service session (Session 0).
func runningInSession0() bool {
	var sess uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sess); err != nil {
		return false
	}
	return sess == 0
}

// gdiCaptureJPEG grabs the primary display with BitBlt and encodes it
// as JPEG at the given quality.
func gdiCaptureJPEG(quality int) ([]byte, error) {
	w, h := fetchDisplayGeometry()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid display geometry %dx%d", w, h)
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
	deferSelect := old
	defer gdiSelectObjectProc.Call(hDC, deferSelect)

	ok, _, berr := gdiBitBltProc.Call(hDC, 0, 0, uintptr(w), uintptr(h), hScreen, 0, 0, gdiSrcCopy)
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt failed: %v", berr)
	}

	// Deselect the bitmap so GetDIBits can read it, then pull the raw
	// pixels and encode in pure Go. The GDI+ file-based encoder returned
	// Win32Error (10) on every save on some systems, so it is gone.
	gdiSelectObjectProc.Call(hDC, old)

	pixels, err := gdiCaptureToBGRA(hDC, hBmp, w, h)
	if err != nil {
		return nil, err
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(pixels); i += 4 {
		img.Pix[i] = pixels[i+2]
		img.Pix[i+1] = pixels[i+1]
		img.Pix[i+2] = pixels[i]
		img.Pix[i+3] = 0xff
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("jpeg encode: %w", err)
	}
	data := buf.Bytes()
	if len(data) == 0 {
		return nil, fmt.Errorf("encoded frame is empty")
	}
	return data, nil
}

// InjectKey synthesizes a key event in this process's session. Exported
// for the per-session remote-control executable, which receives input
// over its own WebSocket and injects it directly (it runs in the
// interactive user session).
func InjectKey(key, event string) {
	synthesizeKey(key, event)
}

// InjectMouse synthesizes a mouse event in this process's session.
// Exported for the remote-control executable (see InjectKey).
func InjectMouse(event string, x, y float64, button string, delta float64) {
	synthesizeMouse(event, x, y, button, delta)
}

// StartCapture begins producing frames on the capture's internal loop
// so Capture() can serve them. Exported for the remote-control
// executable, which runs the loop in its own process.
func (c *windowsCapture) StartCapture(ctx context.Context) error {
	return c.startSession(ctx)
}

// StopCapture stops the internal frame loop.
func (c *windowsCapture) StopCapture() {
	c.stopSession()
}
