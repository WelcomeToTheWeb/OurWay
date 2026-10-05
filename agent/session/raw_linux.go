//go:build linux

package session

import (
	"context"
	"fmt"
	"log"
	"time"
)

// rawInterval paces viewer capture; scrot is a process spawn per frame,
// so this is a ceiling rather than a target.
const rawInterval = 100 * time.Millisecond

// StartCapture starts the viewer frame loop. It idles until a viewer
// attaches (SetViewerMode) so an unattended browser session pays nothing.
func (c *LinuxScreenCapture) StartCapture(ctx context.Context) error {
	if _, err := c.grab(); err != nil {
		return fmt.Errorf("screen capture unavailable: %w", err)
	}
	go c.rawLoop(ctx)
	return nil
}

func (c *LinuxScreenCapture) rawLoop(ctx context.Context) {
	var seq uint64
	for ctx.Err() == nil {
		if !c.viewerMode.Load() {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		start := time.Now()
		img, err := c.grabRGBA()
		if err != nil {
			log.Printf("session: viewer capture failed: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		seq++
		c.rawMu.Lock()
		c.raw = RawFrame{Img: img, Seq: seq, Monitor: 0}
		c.rawMu.Unlock()
		if d := rawInterval - time.Since(start); d > 0 {
			time.Sleep(d)
		}
	}
}

// SetViewerMode implements RawSource.
func (c *LinuxScreenCapture) SetViewerMode(on bool) {
	c.viewerMode.Store(on)
	if !on {
		c.rawMu.Lock()
		c.raw = RawFrame{}
		c.rawMu.Unlock()
	}
}

// Monitors implements RawSource. scrot captures the whole X screen, so
// it is reported as a single monitor.
func (c *LinuxScreenCapture) Monitors() []Monitor {
	w, h := displaySize()
	return []Monitor{{ID: 0, Name: "Display", W: w, H: h, Primary: true}}
}

// SelectMonitor implements RawSource.
func (c *LinuxScreenCapture) SelectMonitor(id int) error {
	if id != 0 && id != MonitorAll {
		return fmt.Errorf("no monitor %d", id)
	}
	return nil
}

// NextRaw implements RawSource.
func (c *LinuxScreenCapture) NextRaw(after uint64, timeout time.Duration) (RawFrame, bool) {
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
