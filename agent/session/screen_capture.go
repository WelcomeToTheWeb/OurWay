package session

// ScreenCapture captures frames from the device's screen.
type ScreenCapture interface {
	// Capture returns the next screen frame as JPEG bytes.
	Capture() ([]byte, error)
	// SetQuality sets the capture quality (0-100).
	SetQuality(quality int)
}
