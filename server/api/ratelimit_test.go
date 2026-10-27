package api

import (
	"testing"
	"time"
)

// The per-user request budget is the ceiling the console is designed to live
// inside, and it is now an operator dial (RATE_LIMIT_PER_MINUTE /
// RATE_LIMIT_BURST). These tests pin the token-bucket contract the dial feeds:
// a bounded instantaneous burst, a sustained refill, and per-user isolation.

func TestRateLimiterDefaultBurstIsOneTenthOfBudget(t *testing.T) {
	rl := NewRateLimiter(time.Minute, 100)

	// Burst 10 of a 100/min budget: the historical behaviour, unchanged.
	for i := 0; i < 10; i++ {
		if !rl.Allow("user:alice") {
			t.Fatalf("request %d denied inside the default burst (10 of 100/min)", i+1)
		}
	}
	if rl.Allow("user:alice") {
		t.Fatal("11th request allowed: the burst is not bounded")
	}
}

func TestRateLimiterHonoursExplicitBurst(t *testing.T) {
	rl := NewRateLimiterWithBurst(time.Minute, 100, 3)

	for i := 0; i < 3; i++ {
		if !rl.Allow("user:alice") {
			t.Fatalf("request %d denied inside the configured burst of 3", i+1)
		}
	}
	if rl.Allow("user:alice") {
		t.Fatal("4th request allowed: an explicit burst of 3 was not honoured")
	}
}

func TestRateLimiterZeroBurstKeepsDefault(t *testing.T) {
	// burst <= 0 must mean "default", so callers passing an unset
	// configuration keep the historical maxCount/10 exactly.
	rl := NewRateLimiterWithBurst(time.Minute, 100, 0)
	for i := 0; i < 10; i++ {
		if !rl.Allow("user:alice") {
			t.Fatalf("request %d denied: burst 0 did not fall back to 10", i+1)
		}
	}
	if rl.Allow("user:alice") {
		t.Fatal("11th request allowed with burst 0")
	}
}

func TestRateLimiterIsPerUser(t *testing.T) {
	rl := NewRateLimiterWithBurst(time.Minute, 10, 2)

	if !rl.Allow("user:alice") || !rl.Allow("user:alice") {
		t.Fatal("alice's burst of 2 was not granted")
	}
	if rl.Allow("user:alice") {
		t.Fatal("alice exceeded her burst")
	}
	// A second user sharing the same installation is not starved by the first.
	if !rl.Allow("user:bob") {
		t.Fatal("bob was denied by alice's spent burst")
	}
}

func TestRateLimiterRefillsAfterTheWindow(t *testing.T) {
	window := 80 * time.Millisecond
	rl := NewRateLimiterWithBurst(window, 10, 1)

	if !rl.Allow("user:alice") {
		t.Fatal("first request denied")
	}
	if rl.Allow("user:alice") {
		t.Fatal("second request allowed inside the burst of 1")
	}
	time.Sleep(2 * window)
	if !rl.Allow("user:alice") {
		t.Fatal("request denied after the window refilled: the limiter is a hard cap, not a token bucket")
	}
}
