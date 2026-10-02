//go:build !windows

package main

import (
	"errors"

	"ourway/agent/config"
)

// isWindowsService reports whether the process is running as a Windows
// service. It is always false outside Windows.
func isWindowsService() bool { return false }

// runServiceMain is a stub on non-Windows platforms.
func runServiceMain(cfg *config.Config) error {
	return errors.New("windows service is only available on Windows")
}
