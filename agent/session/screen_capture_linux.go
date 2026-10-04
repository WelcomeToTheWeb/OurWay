//go:build linux

package session

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
)

// LinuxScreenCapture uses scrot or import to capture the screen.
type LinuxScreenCapture struct {
	quality int
}

// NewScreenCapture creates a new Linux screen capture.
func NewScreenCapture() ScreenCapture {
	return &LinuxScreenCapture{quality: 80}
}

// Capture captures the screen and returns JPEG bytes.
func (c *LinuxScreenCapture) Capture() ([]byte, error) {
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

	// Read PNG and convert to JPEG
	data, err := os.ReadFile(shot)
	if err != nil {
		// If it's already a JPEG from scrot, use it directly
		return nil, fmt.Errorf("failed to read screenshot: %w", err)
	}

	// Decode PNG
	img, err := decodePNG(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot: %w", err)
	}

	// Encode as JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: c.quality}); err != nil {
		return nil, fmt.Errorf("failed to encode JPEG: %w", err)
	}

	return buf.Bytes(), nil
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
