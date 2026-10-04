//go:build !windows

package main

import (
	"fmt"
	"os"
)

// The per-session remote-control executable is Windows-only: the split
// from the RMM agent exists so capture/input can run in the interactive
// user session while the service stays in Session 0.
func main() {
	fmt.Fprintln(os.Stderr, "ourway-remote is only supported on Windows")
	os.Exit(1)
}
