//go:build linux

package session

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder for image.Decode
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// LinuxScreenCapture uses scrot or import to capture the screen. It also
// implements RawSource (raw_linux.go) for the native viewer.
type LinuxScreenCapture struct {
	quality int

	viewerMode atomic.Bool
	rawMu      sync.Mutex
	raw        RawFrame
}

// NewScreenCapture creates a new Linux screen capture.
func NewScreenCapture() ScreenCapture {
	return &LinuxScreenCapture{quality: 80}
}

// Capture captures the screen and returns JPEG bytes.
func (c *LinuxScreenCapture) Capture() ([]byte, error) {
	img, err := c.grab()
	if err != nil {
		return nil, err
	}

	// Encode as JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: c.quality}); err != nil {
		return nil, fmt.Errorf("failed to encode JPEG: %w", err)
	}

	return buf.Bytes(), nil
}

// grab captures the whole X screen as an image.
func (c *LinuxScreenCapture) grab() (image.Image, error) {
	// scrot will not overwrite an existing file (it appends _000), and
	// the path must be private to this uid so the agent (root) and the
	// remote exe (user) do not trip over each other's file.
	shot := fmt.Sprintf("/tmp/ourway_screenshot_%d.png", os.Getuid())
	os.Remove(shot)

	// Try scrot first, fall back to import
	var cmd *exec.Cmd
	if _, err := exec.LookPath("scrot"); err == nil {
		cmd = exec.Command("scrot", "-q", fmt.Sprintf("%d", c.quality), shot)
	} else if _, err := exec.LookPath("import"); err == nil {
		cmd = exec.Command("import", "-window", "root", shot)
	} else {
		return nil, fmt.Errorf("no screen capture tool found (need scrot or import)")
	}

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("screen capture failed: %w", err)
	}

	data, err := os.ReadFile(shot)
	if err != nil {
		return nil, fmt.Errorf("failed to read screenshot: %w", err)
	}
	img, err := decodePNG(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot: %w", err)
	}
	return img, nil
}

// grabRGBA is grab converted to a fresh, origin-based RGBA (the frame
// encoder keeps it as the diff reference, so it must not be reused).
func (c *LinuxScreenCapture) grabRGBA() (*image.RGBA, error) {
	img, err := c.grab()
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	return rgba, nil
}

// SetQuality sets the JPEG quality (0-100).
func (c *LinuxScreenCapture) SetQuality(quality int) {
	if quality < 1 || quality > 100 {
		quality = 80
	}
	c.quality = quality
}

func decodePNG(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}
