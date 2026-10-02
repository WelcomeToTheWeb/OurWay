//go:build !windows

package session

import "log"

// RunUserHelper is only supported on Windows, where the Session 0
// service needs a per-user helper to capture the interactive desktop.
func RunUserHelper(addr string) int {
	log.Printf("user-helper mode is not supported on this platform")
	return 1
}
