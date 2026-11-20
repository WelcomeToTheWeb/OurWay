package alerts

import (
	"testing"

	"ourway/server/models"
)

// TestAutoResolveMarksEachAlertResolvedOnce is the reason the mock was corrected.
//
// While `MockAlertStore.MarkResolved` wrote only `resolved_at`, the `Resolved`
// field that `List(unresolvedOnly)` filters on stayed false for every alert the
// engine resolved. So each subsequent sample got the already-resolved alert back
// in its "unresolved" list, and nothing in this package could tell the difference
// between two very different behaviours:
//
//	the engine resolves it once and leaves it alone, versus
//	the engine re-marks it on every sample and publishes `alert_resolved` again.
//
// Both read identically through a double that never marks anything resolved, and
// the second one is the bug an operator would actually feel: an alert that keeps
// re-resolving itself in the timeline, once per reporting interval, forever. This
// is a case where the missing test was not an untested line but an unrepresentable
// distinction.
func TestAutoResolveMarksEachAlertResolvedOnce(t *testing.T) {
	f := newFleet(t, []string{})
	f.rule(t, models.ScopeDevice, f.device.ID, models.MetricCPU, 50, 65, 0, true)

	f.engine.Evaluate(metrics(70, 0, 0), f.device.ID, f.device.Name)
	alert := f.lastAlertFor(t, f.device.ID, models.MetricCPU)
	if alert.ResolvedAt != nil {
		t.Fatal("the alert was resolved while the metric was still breaching it")
	}
	id := alert.ID

	// Three samples back in the safe range. The first resolves the alert; the two
	// after it must leave it alone.
	for i := 0; i < 3; i++ {
		f.engine.Evaluate(metrics(40, 0, 0), f.device.ID, f.device.Name)
	}

	var calls int
	for _, marked := range f.alerts.resolvedCalls {
		if marked == id {
			calls++
		}
	}
	if calls != 1 {
		t.Errorf("auto-resolution marked the same alert %d times across three safe samples, want 1: %v",
			calls, f.alerts.resolvedCalls)
	}

	// And the resolved state must survive: a later safe sample that re-marked it
	// would also have re-published alert_resolved.
	after := f.lastAlertFor(t, f.device.ID, models.MetricCPU)
	if after.ResolvedAt == nil {
		t.Error("the alert is no longer marked resolved after further safe samples")
	}
}
