//go:build windows

package main

import (
	"log"
	"os"
)

// setupLogging logs to a per-machine file next to the agent's own log
// location so field debugging does not depend on a console that never
// opens (this exe is built with the GUI subsystem).
func setupLogging() {
	logDir := os.Getenv("PROGRAMDATA") + "\\OurWay"
	if err := os.MkdirAll(logDir, 0o755); err == nil {
		if f, err := os.OpenFile(logDir+"\\ourway-remote.log",
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			log.SetOutput(f)
		}
	}
}
