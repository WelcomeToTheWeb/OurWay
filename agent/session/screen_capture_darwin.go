//go:build darwin

package session

import (
	"fmt"
	"os"
	"os/exec"
)

// DarwinScreenCapture uses screencapture to capture the screen.
type DarwinScreenCapture struct {
	quality int
}

// NewScreenCapture creates a new macOS screen capture.
func NewScreenCapture() ScreenCapture {
	return &DarwinScreenCapture{quality: 80}
}

// Capture captures the screen and returns JPEG bytes.
func (c *DarwinScreenCapture) Capture() ([]byte, error) {
	cmd := exec.Command("screencapture", "-x", "-t", "jpg", "-q", fmt.Sprintf("%d", c.quality), "/tmp/ourway_screenshot.jpg")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("screen capture failed: %w", err)
	}

	data, err := os.ReadFile("/tmp/ourway_screenshot.jpg")
	if err != nil {
		return nil, fmt.Errorf("failed to read screenshot: %w", err)
	}

	return data, nil
}

// SetQuality sets the JPEG quality (0-100).
func (c *DarwinScreenCapture) SetQuality(quality int) {
	if quality < 1 || quality > 100 {
		quality = 80
	}
	c.quality = quality
}
