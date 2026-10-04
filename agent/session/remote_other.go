//go:build !windows

package session

// startRemoteSession is Windows-only: elsewhere the in-process capture
// path runs directly.
func startRemoteSession(sm *SessionManager) bool { return false }

// stopRemoteSession is a no-op off Windows.
func stopRemoteSession() {}
