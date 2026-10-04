//go:build !windows

package session

import "errors"

// SendSAS is Windows-only.
func SendSAS() error { return errors.New("send_sas is only supported on Windows") }

// NewClipboard returns nil: clipboard sync is Windows-only for now.
func NewClipboard() Clipboard { return nil }

// EnableDPIAwareness is a no-op outside Windows.
func EnableDPIAwareness() {}
