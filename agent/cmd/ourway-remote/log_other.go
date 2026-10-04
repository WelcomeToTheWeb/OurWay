//go:build !windows

package main

// setupLogging is a no-op off Windows: the agent redirects the exe's
// stdout/stderr to its log file when it spawns it.
func setupLogging() {}
