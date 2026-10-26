package session

import (
	"log"
	"sync"
	"time"
)

// Throttled input logging.
//
// Remote input events arrive at pointer-move rates, and the failure worth
// reporting (no Accessibility grant, no xdotool, an unknown key) repeats on
// every one of them. Logging each is buglist.md M20 — a unit that cannot act
// filled ~1,900 identical lines into its log in two minutes. One line per
// reason per 10 seconds, and a line when it recovers.
type inputLogLimiter struct {
	mu      sync.Mutex
	allowed map[string]time.Time
}

var inputLogs = &inputLogLimiter{allowed: map[string]time.Time{}}

// logThrottled logs reason+message at most once per 10 seconds per reason.
func logThrottled(reason, message string) {
	inputLogs.mu.Lock()
	next, ok := inputLogs.allowed[reason]
	if ok && time.Now().Before(next) {
		inputLogs.mu.Unlock()
		return
	}
	inputLogs.allowed[reason] = time.Now().Add(10 * time.Second)
	inputLogs.mu.Unlock()
	log.Printf("session: %s", message)
}

// logInputDenied names the reason a control event was dropped, once per burst.
func logInputDenied(kind, what string) {
	logThrottled("input-denied", "remote input dropped ("+kind+" "+what+"): this agent lacks the macOS Accessibility permission")
}
