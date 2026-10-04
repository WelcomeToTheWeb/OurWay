//go:build windows

package session

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"sync"

	d3d11 "github.com/kirides/go-d3d/d3d11"
	"github.com/kirides/go-d3d/outputduplication"
)

// dxgiCapture captures the primary output with the Windows Desktop
// Duplication API (IDXGIOutputDuplication) via the go-d3d bindings. The
// GPU compositor hands over the desktop surface directly, which is
// faster than GDI BitBlt and captures hardware-accelerated content.
// Any init or runtime failure permanently falls back to the GDI path,
// so callers never need DXGI-specific error handling.
type dxgiCapture struct {
	mu      sync.Mutex
	inited  bool
	broken  bool
	dup     *outputduplication.OutputDuplicator
	img     *image.RGBA
	bounds  image.Rectangle
	lastJPEG []byte
	lastQ   int
}

var dxgiState dxgiCapture

// dxgiInitLocked sets up the duplication session. Errors are permanent:
// headless machines, Session-0 access denials and pre-W8 drivers all end
// here and the GDI fallback takes over.
func (d *dxgiCapture) initLocked() error {
	d.inited = true
	device, ctx, err := d3d11.NewD3D11Device()
	if err != nil {
		return fmt.Errorf("D3D11 device: %w", err)
	}
	dup, err := outputduplication.NewIDXGIOutputDuplication(device, ctx, 0)
	if err != nil {
		return fmt.Errorf("duplicate output: %w", err)
	}
	dup.DrawPointer = true
	bounds, err := dup.GetBounds()
	if err != nil {
		dup.Release()
		return fmt.Errorf("output bounds: %w", err)
	}
	d.dup = dup
	d.bounds = bounds
	d.img = image.NewRGBA(bounds)
	return nil
}

// Frame returns the current desktop as JPEG. When the screen has not
// changed the previous JPEG is re-served without re-encoding.
func (d *dxgiCapture) Frame(quality int) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.broken {
		return gdiCaptureJPEG(quality)
	}
	if !d.inited {
		if err := d.initLocked(); err != nil {
			d.broken = true
			log.Printf("session: desktop duplication unavailable (%v); using GDI capture", err)
			return gdiCaptureJPEG(quality)
		}
	}
	if err := d.dup.GetImage(d.img, 50); err != nil {
		if err == outputduplication.ErrNoImageYet {
			// No new frame within the timeout: the screen is unchanged.
			// When a previous JPEG exists it is re-served without
			// re-encoding; when it does not (fresh session on an idle
			// desktop) DXGI would never deliver a first frame at all,
			// so seed one via GDI.
			if d.lastJPEG != nil && d.lastQ == quality {
				return d.lastJPEG, nil
			}
			data, gerr := gdiCaptureJPEG(quality)
			if gerr != nil {
				return nil, errDXGIFrameStale
			}
			d.lastJPEG = data
			d.lastQ = quality
			return data, nil
		}
		// Access lost (mode change, session switch) or a hard error:
		// rebuild once, then give up to GDI.
		d.broken = true
		d.dup.Release()
		d.dup = nil
		log.Printf("session: desktop duplication failed (%v); using GDI capture", err)
		return gdiCaptureJPEG(quality)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, d.img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("jpeg encode: %w", err)
	}
	d.lastJPEG = buf.Bytes()
	d.lastQ = quality
	return d.lastJPEG, nil
}

var errDXGIFrameStale = fmt.Errorf("dxgi: no new frame")
