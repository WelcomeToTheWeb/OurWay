//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "ourway-viewer is Windows-only for now")
	os.Exit(1)
}
