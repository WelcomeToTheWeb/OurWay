package alerts

import (
	"testing"

	"ourway/server/models"
)

// 2.4-0 · agent metric **sequences** that cross the thresholds.
//
// The precedence tests (threshold_test.go) feed the engine one synthetic point —
// `metrics(62, 72, 88)` — and ask which rule owns the alert. That is the right
// question for precedence, and it cannot answer the other one an operator asks:
// a unit climbs over a threshold, sits there, climbs further, then comes back
// down. What does the alert list look like at each of those moments? Which of the
// four behaviours — fire, suppress, escalate, auto-resolve — is the engine
// actually doing, and does the rule that wins stay the same one at every crossing?
//
// So the sequences below are written to cross thresholds on purpose, and the
// assertions are per step, not per end state.
//
// **These are hand-built, not captured.** They are written against the numbers
// this engine ships with — cpu 80/90, ram 85/95, disk 85/95 as the built-in floor
// (`NewEngine`) — and the device/tag rules each test installs. Nothing here came
// from a macOS or Linux host: no hardware and no agent run was involved, and
// claiming otherwise would repeat the mistake `agent/patch/testdata` was
// corrected for. What the two series take from their platform label is the
// *shape* of the values — see the single-core note on the darwin series — not a
// transcript.
//
// Why a Go table and not a fixture file: `server/` and `agent/` are separate Go
// modules, so an agent-side fixture cannot be imported by these tests, and a file
// fixture would need a parser whose first compiler is CI. The table is the same
// data with one fewer unverifiable moving part. The deviation is recorded against
// the task in `docs/phase2.4-tasks.md`.

// want is what one metric must look like after one sample: the metric, the
// severity it raised ("" means it must have raised no alert at all), the
// threshold that won — which is the precedence result — and whether that alert
// has been auto-resolved. Every metric in every step is stated, including the
// ones that must stay silent: a table where an omitted metric means "don't care"
// is a table that quietly stops testing the cases nobody thought to write down.
type want struct {
	metric    string
	severity  string
	threshold float64
	resolved  bool
}

// step is one agent sample and what the engine must have done with it. `note` is
// why the sample exists and appears in every failure message, so a red run says
// which behaviour broke rather than just which float disagreed.
type step struct {
	cpu, ram, disk float64
	note           string
	want           []want
}

// quiet is the expectation that a metric stayed silent.
func quiet(metric string) want {
	return want{metric: metric}
}

// fired is the expectation that a metric raised `severity` attributed to
// `threshold` — that pair is the precedence result, so it is asserted at every
// crossing rather than only at the first one.
func fired(metric, severity string, threshold float64) want {
	return want{metric: metric, severity: severity, threshold: threshold}
}

// stillFired is the same alert, now auto-resolved because the metric came back
// under the warning that owns it.
func resolved(metric, severity string, threshold float64) want {
	return want{metric: metric, severity: severity, threshold: threshold, resolved: true}
}

func TestLinuxMetricSequenceClimbsThroughWarningToCriticalAndBack(t *testing.T) {
	f := newFleet(t, []string{"prod"})
	// The precedence under test: the device rule tightest, the tenant's tag rule
	// next, and disk deliberately left to the built-in floor.
	f.rule(t, models.ScopeDevice, f.device.ID, models.MetricCPU, 50, 65, 0, true)
	f.rule(t, models.ScopeTag, "prod", models.MetricRAM, 60, 75, 0, true)

	steps := []step{
		{
			cpu: 30, ram: 40, disk: 60,
			note: "idle: every metric under every threshold",
			want: []want{quiet(models.MetricCPU), quiet(models.MetricRAM), quiet(models.MetricDisk)},
		},
		{
			cpu: 52,
			note: "cpu crosses the device rule's warning (50) and nothing else moves",
			want: []want{fired(models.MetricCPU, "warning", 50), quiet(models.MetricRAM), quiet(models.MetricDisk)},
		},
		{
			cpu: 53,
			note: "cpu still warning one sample later: the dedup window must swallow the repeat, not stack a second alert",
			want: []want{fired(models.MetricCPU, "warning", 50), quiet(models.MetricRAM), quiet(models.MetricDisk)},
		},
		{
			cpu: 67,
			note: "cpu crosses the device rule's critical (65): a new severity fires even inside the dedup window, because the key carries the severity",
			want: []want{fired(models.MetricCPU, "critical", 65), quiet(models.MetricRAM), quiet(models.MetricDisk)},
		},
		{
			cpu: 67, ram: 62, disk: 86,
			note: "ram crosses the tag rule's warning (60, not the tenant's 70); disk crosses the built-in floor (85) with no rule at all",
			want: []want{fired(models.MetricCPU, "critical", 65), fired(models.MetricRAM, "warning", 60), fired(models.MetricDisk, "warning", 85)},
		},
		{
			cpu: 40, ram: 40, disk: 86,
			note: "cpu and ram come back under their resolved warnings; disk stays where it was and its alert must not silently clear",
			want: []want{resolved(models.MetricCPU, "critical", 65), resolved(models.MetricRAM, "warning", 60), fired(models.MetricDisk, "warning", 85)},
		},
	}

	for i, s := range steps {
		f.engine.Evaluate(metrics(s.cpu, s.ram, s.disk), f.device.ID, f.device.Name)
		assertStep(t, f, i, s)
	}
}

func TestDarwinMetricSequenceDoesNotAlertOnOneBusyCore(t *testing.T) {
	// The reason a second platform series exists rather than a longer linux one:
	// the macOS collector reports CPU as a share of the **whole machine**, so one
	// pegged core on an 8-core host reads about 12.5%. A sequence that treated
	// that as a warning would have the console crying over a single runaway
	// process on every laptop in the fleet. So this series crosses the device rule
	// the way a real Mac does — several cores at once — and its first sample is
	// the case that must stay silent.
	f := newFleet(t, nil)
	f.rule(t, models.ScopeDevice, f.device.ID, models.MetricCPU, 50, 65, 0, true)

	silent := []want{quiet(models.MetricCPU), quiet(models.MetricRAM), quiet(models.MetricDisk)}

	steps := []step{
		{cpu: 12.5, note: "one core of eight pegged on an 8-core host: must not alert", want: silent},
		{cpu: 55, note: "several cores loaded at once: crosses the device rule's warning", want: []want{fired(models.MetricCPU, "warning", 50), quiet(models.MetricRAM), quiet(models.MetricDisk)}},
		{cpu: 55, note: "same severity, one sample later: suppressed by the dedup window", want: []want{fired(models.MetricCPU, "warning", 50), quiet(models.MetricRAM), quiet(models.MetricDisk)}},
		{cpu: 91, note: "build box saturating: crosses the device rule's critical (65)", want: []want{fired(models.MetricCPU, "critical", 65), quiet(models.MetricRAM), quiet(models.MetricDisk)}},
		{cpu: 51, note: "back under the critical but still over the warning: auto-resolution measures the **warning**, so a critical alert that has not truly cooled must stay open", want: []want{fired(models.MetricCPU, "critical", 65), quiet(models.MetricRAM), quiet(models.MetricDisk)}},
	}

	for i, s := range steps {
		f.engine.Evaluate(metrics(s.cpu, 0, 0), f.device.ID, f.device.Name)
		assertStep(t, f, i, s)
	}
}

// assertStep compares the engine's visible state after one sample against what
// that sample was written to do. Severity and threshold are checked rather than
// the message text: the message format has its own tests, and an assertion on it
// here would fail for the wrong reason.
func assertStep(t *testing.T, f *fleet, i int, s step) {
	t.Helper()
	for _, metric := range []string{models.MetricCPU, models.MetricRAM, models.MetricDisk} {
		w, found := expectationFor(s.want, metric)
		if !found {
			t.Fatalf("step %d (%s) states no expectation for %q: every metric must be declared, even as quiet()", i, s.note, metric)
		}
		checkMetric(t, f, i, s.note, w)
	}
}

func expectationFor(list []want, metric string) (want, bool) {
	for _, w := range list {
		if w.metric == metric {
			return w, true
		}
	}
	return want{}, false
}

func checkMetric(t *testing.T, f *fleet, i int, note string, w want) {
	t.Helper()
	a := newestAlert(f, f.device.ID, w.metric)
	if w.severity == "" {
		if a != nil {
			t.Errorf("step %d (%s): %s should have raised nothing, raised %s at %v",
				i, note, w.metric, a.Severity, a.Threshold)
		}
		return
	}
	if a == nil {
		t.Fatalf("step %d (%s): %s should have raised %s at %v, raised nothing",
			i, note, w.metric, w.severity, w.threshold)
	}
	if a.Severity != w.severity {
		t.Errorf("step %d (%s): %s severity = %q, want %q", i, note, w.metric, a.Severity, w.severity)
	}
	if a.Threshold != w.threshold {
		t.Errorf("step %d (%s): %s threshold = %v, want %v — which rule wins has to be readable in the alert the operator finds",
			i, note, w.metric, a.Threshold, w.threshold)
	}
	if a.Resolved != w.resolved {
		t.Errorf("step %d (%s): %s resolved = %v, want %v", i, note, w.metric, a.Resolved, w.resolved)
	}
}

// newestAlert returns the most recently recorded alert for one device and metric,
// or nil when none has ever been raised. Unlike the precedence tests'
// lastAlertFor, "nothing yet" is a result worth distinguishing here: several of
// these steps exist to prove the engine stayed quiet.
func newestAlert(f *fleet, deviceID, metric string) *models.Alert {
	var found *models.Alert
	for i := range f.alerts.alerts {
		if a := f.alerts.alerts[i]; a != nil && a.DeviceID == deviceID && a.Metric == metric {
			found = a
		}
	}
	return found
}
