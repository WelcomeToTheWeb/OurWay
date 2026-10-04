//go:build windows

package session

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

// helperFrameInterval is the capture cadence used both by the per-user
// helper and by local (non-service) capture: ~15fps.
const helperFrameInterval = 70 * time.Millisecond

// windowsCapture captures the primary display with a native GDI BitBlt
// and encodes frames as JPEG (no per-frame process spawn).
//
// When the agent runs as a Windows service (Session 0) the desktop is
// not accessible to this process; a per-user helper (this same binary
// re-launched with --user-helper in the active console session) then
// does the capturing and input synthesis and streams frames back over
// loopback TCP. In both modes Capture() serves the most recent frame
// from the internal 15fps producer, so the code path is identical.
type windowsCapture struct {
	quality atomic.Int32

	mu              sync.Mutex
	helper          *userHelper
	localLoopActive bool
	localStop       chan struct{}

	lastFrame atomic.Value // []byte
	lastAt    atomic.Int64 // unix nano
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
	// In helper mode the frames are encoded by the helper process, so
	// the quality must be forwarded to it.
	if h := c.helperRef(); h != nil {
		_ = h.send(map[string]interface{}{"type": "quality", "quality": quality})
	}
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

// startSession starts the frame producer: the per-user helper when
// running in Session 0, a local capture loop otherwise.
func (c *windowsCapture) startSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.helper != nil || c.localLoopActive {
		return nil
	}

	if !runningInSession0() {
		c.startLocalLoopLocked()
		return nil
	}

	h, err := c.spawnHelperLocked()
	if err != nil {
		log.Printf("session: per-user helper failed (%v); using local capture (screen will be blank in Session 0)", err)
		c.startLocalLoopLocked()
		return nil
	}
	c.helper = h
	// The helper idles until it receives "start": only then does its
	// capture loop produce frames.
	if err := h.send(map[string]interface{}{"type": "start"}); err != nil {
		log.Printf("session: failed to start helper capture: %v", err)
	}
	return nil
}

// stopSession stops the frame producer and terminates the helper.
func (c *windowsCapture) stopSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h := c.helper; h != nil {
		c.helper = nil
		_ = h.send(map[string]interface{}{"type": "stop"})
		h.close()
	}
	if c.localLoopActive {
		c.localLoopActive = false
		close(c.localStop)
		c.localStop = make(chan struct{})
	}
}

// inputKey routes a key event to the helper when active, else injects
// it locally via SendInput.
func (c *windowsCapture) inputKey(key, event string) {
	if h := c.helperRef(); h != nil {
		_ = h.send(map[string]interface{}{
			"type":  "input",
			"input": map[string]interface{}{"type": "key", "key": key, "event": event},
		})
		return
	}
	synthesizeKey(key, event)
}

// inputMouse routes a mouse event to the helper when active, else
// injects it locally via SendInput.
func (c *windowsCapture) inputMouse(event string, x, y float64, button string, delta float64) {
	if h := c.helperRef(); h != nil {
		_ = h.send(map[string]interface{}{
			"type":  "input",
			"input": map[string]interface{}{"type": "mouse", "event": event, "x": x, "y": y, "button": button, "delta": delta},
		})
		return
	}
	synthesizeMouse(event, x, y, button, delta)
}

func (c *windowsCapture) helperRef() *userHelper {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.helper
}

func (c *windowsCapture) startLocalLoopLocked() {
	if c.localLoopActive {
		return
	}
	c.localLoopActive = true
	stop := c.localStop
	go func() {
		// First frame immediately so the viewer does not sit on
		// "waiting for first frame".
		c.pushLocalFrame()
		tick := time.NewTicker(helperFrameInterval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				c.pushLocalFrame()
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
