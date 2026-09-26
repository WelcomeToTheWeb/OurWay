//go:build windows

package session

import (
	"encoding/base64"
	"fmt"
	"os/exec"
)

// WindowsScreenCapture uses PowerShell or GDI to capture the screen.
type WindowsScreenCapture struct {
	quality int
}

// NewScreenCapture creates a new Windows screen capture.
func NewScreenCapture() ScreenCapture {
	return &WindowsScreenCapture{quality: 80}
}

// Capture captures the screen and returns JPEG bytes.
func (c *WindowsScreenCapture) Capture() ([]byte, error) {
	// Use PowerShell with GDI to capture the primary screen
	script := `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$screen = [System.Windows.Forms.Screen]::PrimaryScreen
$bmp = New-Object System.Drawing.Bitmap $screen.Bounds.Width, $screen.Bounds.Height
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($screen.Bounds.Location, [System.Drawing.Point]::Empty, $screen.Bounds.Size)
$ms = New-Object System.IO.MemoryStream
$bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Jpeg)
$bmp.Dispose()
$g.Dispose()
return [System.Convert]::ToBase64String($ms.ToArray())
`
	cmd := exec.Command("powershell", "-Command", script)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("screen capture failed: %w", err)
	}

	// Decode base64
	encoded := string(output)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode screenshot: %w", err)
	}

	return data, nil
}

// SetQuality sets the JPEG quality (0-100).
func (c *WindowsScreenCapture) SetQuality(quality int) {
	if quality < 1 || quality > 100 {
		quality = 80
	}
	c.quality = quality
}
