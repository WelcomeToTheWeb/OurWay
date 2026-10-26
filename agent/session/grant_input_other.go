//go:build !darwin

package session

// requestInputGrant is a no-op outside macOS: only macOS gates synthetic input
// behind a user grant (Accessibility / TCC).
func requestInputGrant() {}
