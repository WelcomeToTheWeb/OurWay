package metrics

import (
	"strconv"
	"testing"
	"time"
)

// Tests for the operational counters (2.5-0).
//
// The registry is process-global state — which is exactly what a server asked
// "how many frames did you drop" needs, and exactly what makes a naive test
// order-dependent. So every test here uses metric names nothing else in this
// package touches, and anything reading a name the production code also moves
// (the Redis transitions, the alert histogram) asserts on the **difference**
// between a reading taken before the act and one after. A test that asserts an
// absolute value of shared state passes alone and fails the first time another
// test runs before it.
//
// What is *not* tested here, stated rather than hidden: the drop sites
// themselves. Moving `ws.broadcast.full` means filling a client's send buffer
// inside a live hub, which is a `server/ws` test with a real connection, and that
// is owed on this change. These tests establish that the thing those sites move
// is correct — a name accumulates, a gauge goes down, a histogram bucket is
// cumulative, a log throttle throttles — so a mis-wired call site is the only
// remaining way to be wrong.

var uniqueSeq int

func uniqueName(prefix string) string {
	uniqueSeq++
	return prefix + "_" + strconv.Itoa(uniqueSeq)
}

func TestUntouchedMetricReadsZero(t *testing.T) {
	// The honest answer for both shapes: nothing counted, nothing queued. A
	// health endpoint that reported "missing" instead would make every fleet look
	// like it had never dropped anything *or* had anything to report on.
	if got := Value(uniqueName("never.written"), nil); got != 0 {
		t.Fatalf("an untouched counter reads %v, want 0", got)
	}
}

func TestAddAccumulatesAndIgnoresZero(t *testing.T) {
	name := uniqueName("accumulate")
	Inc(name)
	Add(name, nil, 3)
	if got := Value(name, nil); got != 4 {
		t.Fatalf("after Inc + Add(3) the counter is %v, want 4", got)
	}
	Add(name, nil, 0)
	if got := Value(name, nil); got != 4 {
		t.Fatalf("Add(0) must be a no-op, got %v", got)
	}
}

func TestLabelsAreCanonicalisedSoOrderDoesNotSplitASeries(t *testing.T) {
	// The same map written two ways is one series. Without canonicalisation the
	// snapshot shows two half-counts for one fact, and the console's panel picks
	// one of them at random depending on Go's map iteration order — which is the
	// kind of nondeterminism a fleet operator reads as the server being flaky.
	name := uniqueName("labelled")
	Add(name, Labels{"role": "user", "kind": "deploy"}, 2)
	Add(name, Labels{"kind": "deploy", "role": "user"}, 3)
	if got := Value(name, Labels{"role": "user", "kind": "deploy"}); got != 5 {
		t.Fatalf("label order split one series: got %v, want 5", got)
	}
	// A different label value is a different series, and must not leak in.
	Add(name, Labels{"role": "device", "kind": "deploy"}, 7)
	if got := Value(name, Labels{"role": "user", "kind": "deploy"}); got != 5 {
		t.Fatalf("role=device leaked into role=user: got %v, want 5", got)
	}
	if got := Value(name, Labels{"role": "device", "kind": "deploy"}); got != 7 {
		t.Fatalf("role=device did not get its own series: got %v, want 7", got)
	}
}

func TestGaugeGoesDownAndSnapshotKeepsTheShapesApart(t *testing.T) {
	// A counter's absolute value means nothing; a gauge's value is the fact. If
	// the shapes collapse into one list, a reader that divides by elapsed time
	// (correct for a counter) prints nonsense for a queue depth, and one that
	// reads the number directly (correct for a gauge) reads a running total as a
	// depth. Carrying a kind is the whole reason the split exists.
	gauge := uniqueName("gauge.shape")
	counter := uniqueName("counter.shape")
	SetGauge(gauge, nil, 10)
	AddGauge(gauge, nil, -3)
	if got := Value(gauge, nil); got != 7 {
		t.Fatalf("gauge after Set(10)+Add(-3) is %v, want 7", got)
	}
	Inc(counter)

	snap := Snapshot()
	if !containsMetric(snap.Gauges, gauge) {
		t.Fatalf("the gauge is not in snapshot.Gauges: %+v", snap.Gauges)
	}
	if containsMetric(snap.Counters, gauge) {
		t.Fatal("a gauge appeared under snapshot.Counters")
	}
	if !containsMetric(snap.Counters, counter) {
		t.Fatalf("the counter is not in snapshot.Counters: %+v", snap.Counters)
	}
}

func containsMetric(list []Metric, name string) bool {
	for _, m := range list {
		if m.Name == name {
			return true
		}
	}
	return false
}

func TestAlertHistogramCountsAreCumulativeAndTheSumAddsUp(t *testing.T) {
	// Prometheus' `le` grammar: bucket i holds every observation ≤ buckets[i].
	// 2.5-1 prints these directly, and a reader computing p95 from
	// non-cumulative buckets gets a percentile that is quietly wrong — the kind
	// of wrong that reads as "we are inside the deadline", which is M5.
	beforeCount, beforeSum, beforeCounts := alertEvalReading(t)

	observations := []time.Duration{
		500 * time.Microsecond, // ≤ 0.001
		20 * time.Millisecond,  // ≤ 0.025
		2 * time.Second,        // ≤ 2.5
		30 * time.Second,       // past every bound
	}
	var wantSum float64
	for _, d := range observations {
		ObserveAlertEval(d)
		wantSum += d.Seconds()
	}

	h := requireHistogram(t, AlertEvalLatency)
	if h.Count-beforeCount != uint64(len(observations)) {
		t.Fatalf("histogram observed %d, want %d", h.Count-beforeCount, len(observations))
	}
	for i := 1; i < len(h.Counts); i++ {
		if h.Counts[i] < h.Counts[i-1] {
			t.Fatalf("bucket %d (%v) is less than bucket %d (%v): counts are not cumulative",
				i, h.Counts[i], i-1, h.Counts[i-1])
		}
	}
	// Each bound is compared against **its own** baseline, not against the total:
	// another test may already have observed into this histogram, and subtracting
	// a total from a bucket is a comparison of two different quantities that only
	// looks right while the suite runs in one particular order.
	grew := make([]uint64, len(h.Counts))
	for i := range h.Counts {
		if i < len(beforeCounts) {
			grew[i] = h.Counts[i] - beforeCounts[i]
		} else {
			grew[i] = h.Counts[i]
		}
	}
	if grew[0] != 1 {
		t.Fatalf("the 0.001 s bucket grew by %v, want 1: only the sub-millisecond observation is ≤ 0.001", grew[0])
	}
	// The bucket list is {0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1,
	// 2.5, 5, 10}, so against the four observations above: 20 ms lands at index 3
	// (0.025) and nowhere earlier, 2 s lands at index 9 (2.5), and 30 s is past
	// every bound. Writing these from the buckets rather than from the
	// observations is the point — the reader of a failure should be able to check
	// the arithmetic without re-deriving which index is which.
	if grew[1] != 1 {
		t.Fatalf("the 0.005 s bucket grew by %v, want 1", grew[1])
	}
	if grew[2] != 1 {
		t.Fatalf("the 0.01 s bucket grew by %v, want 1: 20 ms is not ≤ 10 ms", grew[2])
	}
	if grew[3] != 2 {
		t.Fatalf("the 0.025 s bucket grew by %v, want 2", grew[3])
	}
	if grew[8] != 2 {
		t.Fatalf("the 1 s bucket grew by %v, want 2", grew[8])
	}
	if grew[9] != 3 {
		t.Fatalf("the 2.5 s bucket grew by %v, want 3", grew[9])
	}
	if last := len(grew) - 1; grew[last] != 3 {
		t.Fatalf("the widest bucket (10 s) grew by %v, want 3: the 30 s observation sits past every bound", grew[last])
	}
	if diff := (h.Sum - beforeSum) - wantSum; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("the sum grew by %v, want %v", h.Sum-beforeSum, wantSum)
	}
}

func TestSnapshotCopiesTheHistogramSoAReaderCannotCorruptIt(t *testing.T) {
	// Two consumers are promised this registry: `/metrics` and the health panel.
	// If they shared the live slices, one sorting in place would change what the
	// other prints, and it would only be visible in production.
	ObserveAlertEval(time.Millisecond)
	first := requireHistogram(t, AlertEvalLatency)
	first.Counts[0] = 99999
	first.Buckets[0] = -1

	second := requireHistogram(t, AlertEvalLatency)
	if second.Counts[0] == 99999 || second.Buckets[0] == -1 {
		t.Fatal("the snapshot shares its slices with the registry: a reader can corrupt the next one")
	}
}

func TestAllowLogAllowsOncePerWindowPerKey(t *testing.T) {
	// This is the "a drop storm is not a log storm" requirement. The window must
	// actually open again: a throttle that only ever allows the first line hides
	// an incident that outlives its first burst, which is the failure mode
	// buglist.md M8/M10 were written about.
	key := uniqueName("logkey")
	window := 30 * time.Second
	advance := withFakeClock(t, time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))

	if !AllowLog(key, window) {
		t.Fatal("the first drop in a window must be logged")
	}
	advance(10 * time.Second)
	if AllowLog(key, window) {
		t.Fatal("a second drop inside the window must not log again")
	}
	advance(25 * time.Second) // 35 s after the first: past the window
	if !AllowLog(key, window) {
		t.Fatal("the window must reopen; permanent silence hides an ongoing incident")
	}
	// Keys are independent: a viewer freezing must not mute the alert engine's
	// line beside it.
	other := uniqueName("logkey")
	if !AllowLog(other, window) {
		t.Fatal("a different key has its own window")
	}
}

func TestRedisTransitionsCountTheIncidentNotTheFailedPublish(t *testing.T) {
	// A Redis that stays down keeps failing. Counting one degradation per failed
	// publish tells an operator about four hundred incidents when there was one,
	// and the transition count stops being something a human can act on.
	degradedBefore := Value(RedisDegradedTransitions, nil)
	recoveredBefore := Value(RedisRecoveredTransitions, nil)

	for i := 0; i < 10; i++ {
		RedisDegraded()
	}
	if grew := Value(RedisDegradedTransitions, nil) - degradedBefore; grew != 1 {
		t.Fatalf("10 failed operations counted %v degradations, want 1", grew)
	}
	if got := Value(RedisDegradedState, nil); got != 1 {
		t.Fatalf("while degraded the state gauge is %v, want 1", got)
	}
	for i := 0; i < 10; i++ {
		RedisRecovered()
	}
	if grew := Value(RedisRecoveredTransitions, nil) - recoveredBefore; grew != 1 {
		t.Fatalf("10 successful operations counted %v recoveries, want 1", grew)
	}
	if got := Value(RedisDegradedState, nil); got != 0 {
		t.Fatalf("after recovery the state gauge is %v, want 0", got)
	}
}

func TestSnapshotIsSortedStablyAcrossReads(t *testing.T) {
	// Two readers of unchanged state must see the same order, or a diff of two
	// `/metrics` scrapes shows movement that never happened.
	a := uniqueName("sorted.a")
	b := uniqueName("sorted.b")
	Inc(b)
	Inc(a)

	first := Snapshot()
	second := Snapshot()
	if len(first.Counters) != len(second.Counters) {
		t.Fatal("two snapshots of unchanged state have different lengths")
	}
	for i := range first.Counters {
		if first.Counters[i].Name != second.Counters[i].Name {
			t.Fatalf("snapshot order is not stable at %d: %q then %q",
				i, first.Counters[i].Name, second.Counters[i].Name)
		}
	}
	for i := 1; i < len(first.Counters); i++ {
		if first.Counters[i-1].Name > first.Counters[i].Name {
			t.Fatalf("counters are not sorted by name at %d: %q then %q",
				i, first.Counters[i-1].Name, first.Counters[i].Name)
		}
	}
}

// --- helpers ------------------------------------------------------------

// alertEvalReading reads the alert histogram's total, sum and per-bucket counts,
// tolerating the histogram not existing yet: the first test to observe into it has
// no baseline to find, and demanding one would make the suite order-dependent —
// the exact trap this file is written to avoid.
func alertEvalReading(t *testing.T) (uint64, float64, []uint64) {
	t.Helper()
	for _, h := range Snapshot().Histograms {
		if h.Name == AlertEvalLatency {
			return h.Count, h.Sum, h.Counts
		}
	}
	return 0, 0, nil
}

func requireHistogram(t *testing.T, name string) *Histogram {
	t.Helper()
	snap := Snapshot()
	for i := range snap.Histograms {
		if snap.Histograms[i].Name == name {
			return &snap.Histograms[i]
		}
	}
	t.Fatalf("no histogram named %q in the snapshot", name)
	return nil
}

// withFakeClock replaces the package clock for one test, restores the real one on
// cleanup, and hands back a way to move time forward. Production never reassigns
// logClockNow; a test is the only thing that needs a window to pass without
// sleeping through it.
func withFakeClock(t *testing.T, start time.Time) func(time.Duration) {
	t.Helper()
	previous := logClockNow
	current := start
	logClockNow = func() time.Time { return current }
	t.Cleanup(func() { logClockNow = previous })
	return func(d time.Duration) { current = current.Add(d) }
}
