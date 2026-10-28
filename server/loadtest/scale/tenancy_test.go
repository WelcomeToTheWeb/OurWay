//go:build loadtest

package main

import (
	"testing"
	"time"
)

// 2.3-8 — the verdict arithmetic.
//
// The load tool's own output is the release criterion: "scoped p95 within 10% of
// the unscoped baseline, zero failures". A campaign needs a server, but the
// judgment needs nothing, and it is the judgment that decides whether a release
// is green. A percentile that picks the wrong slot, or a budget computed off the
// mean, says PASS about a build that fails at a real MSP's size — and the
// measurement it is quoting is a real measurement of the wrong thing.

func stat(name string, latencies ...time.Duration) *endpointStat {
	s := &endpointStat{name: name}
	s.latency = append(s.latency, latencies...)
	return s
}

func TestPercentilePicksTheRightSlot(t *testing.T) {
	// 100 samples, 1 ms … 100 ms. The tool's percentile is `sorted[(n-1)*p/100]`:
	// p50 is index 49 (50 ms) and p95 is index 94 (95 ms) — not the largest
	// value, and not a mean the campaign never measured.
	s := stat("/api/devices")
	for i := 1; i <= 100; i++ {
		s.latency = append(s.latency, time.Duration(i)*time.Millisecond)
	}
	if got, want := s.p50(), 50*time.Millisecond; got != want {
		t.Errorf("p50 = %s, want %s", got, want)
	}
	if got, want := s.p95(), 95*time.Millisecond; got != want {
		t.Errorf("p95 = %s, want %s", got, want)
	}

	// Unsorted input: the percentile is a property of the distribution, not of
	// the order the samples arrived in.
	shuffled := stat("/api/alerts", 30*time.Millisecond, 10*time.Millisecond, 20*time.Millisecond)
	if got, want := shuffled.p50(), 20*time.Millisecond; got != want {
		t.Errorf("p50 of unsorted samples = %s, want %s", got, want)
	}

	// No samples answers zero. A campaign that measured nothing must not
	// present a latency, and the verdict skips an endpoint with no baseline
	// samples rather than comparing against zero.
	if got := stat("/api/empty").p95(); got != 0 {
		t.Errorf("p95 with no samples = %s, want 0", got)
	}
}

func TestReportTenancyVerdict(t *testing.T) {
	result := func(s *endpointStat) *campaignResult {
		return &campaignResult{actor: s.name, endpoints: []*endpointStat{s}}
	}
	base := result(stat("/api/devices", 500*time.Millisecond))

	// Within the 10% budget, no failures: PASS.
	ok := stat("/api/devices", 540*time.Millisecond)
	ok.requests, ok.failures = 100, 0
	if !reportTenancy(base, result(ok)) {
		t.Error("a scoped read inside the 10% budget must pass")
	}

	// Over the budget: FAIL. 551 ms is over 500 ms × 1.1.
	over := stat("/api/devices", 551*time.Millisecond)
	over.requests, over.failures = 100, 0
	if reportTenancy(base, result(over)) {
		t.Error("a scoped read over the 10% budget must fail")
	}

	// Zero failures is a criterion of its own: a fast read that 500s is not a
	// pass, whatever its latency says.
	failing := stat("/api/devices", 10*time.Millisecond)
	failing.requests, failing.failures = 100, 1
	if reportTenancy(base, result(failing)) {
		t.Error("a campaign with a failed request must not pass")
	}

	// No baseline samples: the comparison is not made, and the endpoint does
	// not turn the verdict red.
	emptyBase := result(stat("/api/devices"))
	if !reportTenancy(emptyBase, result(ok)) {
		t.Error("an endpoint with no baseline samples must not fail the verdict")
	}
}
