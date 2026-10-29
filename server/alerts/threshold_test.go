package alerts

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/models"
	"ourway/server/store"
)

// 2.4-5 · threshold precedence.
//
// The rule: **device → tag → client → global, most specific wins, per metric
// independently.** A lab machine's cpu rule must not move a client's disk rule,
// and "which rule fired" has to be readable in the alert the operator finds.
//
// These tests run against the real rule store, not a stub of it: the precedence
// arithmetic is the store's, and a mock that agrees with the engine proves
// nothing about the arithmetic.

type fleet struct {
	engine  *Engine
	alerts  *MockAlertStore
	rules   *store.ThresholdStore
	device  *models.Device
	client  *models.Client
	device2 *models.Device
}

func newFleet(t *testing.T, tags []string) *fleet {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	st, err := store.NewWithDB(db)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	client, err := st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Alpha Co", Slug: "alpha-co"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	device := &models.Device{
		ID: uuid.NewString(), Name: "alpha-01", Hostname: "alpha-01", OS: "linux", Arch: "amd64",
		Status: "online", Tags: tags, DeviceKey: uuid.NewString(), ClientID: &client.ID,
	}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("device: %v", err)
	}
	// A second unit in the same tenant, with no tags: the test that "only the
	// device I named moved" needs a unit that was not named.
	device2 := &models.Device{
		ID: uuid.NewString(), Name: "alpha-02", Hostname: "alpha-02", OS: "linux", Arch: "amd64",
		Status: "online", Tags: []string{}, DeviceKey: uuid.NewString(), ClientID: &client.ID,
	}
	if err := st.Devices.Create(device2); err != nil {
		t.Fatalf("second device: %v", err)
	}

	mock := &MockAlertStore{}
	engine := NewEngine(mock)
	WireDeviceScopes(engine, st)
	return &fleet{engine: engine, alerts: mock, rules: st.Thresholds, device: device, client: client, device2: device2}
}

// rule seeds one threshold rule.
func (f *fleet) rule(t *testing.T, scopeType, scopeValue, metric string, warning, critical float64, dedupSeconds int, enabled bool) {
	t.Helper()
	row := &models.AlertThreshold{
		ScopeType: scopeType, ScopeValue: scopeValue, Metric: metric,
		Warning: warning, Critical: critical, DedupWindowSeconds: dedupSeconds, Enabled: enabled,
	}
	if scopeType == models.ScopeTag {
		row.ScopeValue = models.NormalizeTags([]string{scopeValue})[0]
	}
	if err := f.rules.Create(row); err != nil {
		t.Fatalf("create rule %s/%s/%s: %v", scopeType, scopeValue, metric, err)
	}
}

// lastAlertFor reads the newest alert on a metric.
func (f *fleet) lastAlertFor(t *testing.T, deviceID, metric string) *models.Alert {
	t.Helper()
	var found *models.Alert
	for i := range f.alerts.alerts {
		if a := f.alerts.alerts[i]; a != nil && a.DeviceID == deviceID && a.Metric == metric {
			found = a
		}
	}
	if found == nil {
		t.Fatalf("no alert was raised for metric %q (alerts: %d)", metric, len(f.alerts.alerts))
	}
	return found
}

func metrics(cpu, ram, disk float64) models.Metrics {
	return models.Metrics{CPU: cpu, RAM: ram, DiskUsage: disk}
}

// The plan's precedence case, in the plan's numbers.
func TestThresholdPrecedenceUsesTheMostSpecificRulePerMetric(t *testing.T) {
	f := newFleet(t, []string{"prod"})
	f.rule(t, models.ScopeGlobal, "", models.MetricCPU, 80, 90, 0, true)
	f.rule(t, models.ScopeClient, f.client.ID, models.MetricCPU, 70, 85, 0, true)
	f.rule(t, models.ScopeTag, "prod", models.MetricCPU, 60, 75, 0, true)
	f.rule(t, models.ScopeDevice, f.device.ID, models.MetricCPU, 50, 65, 0, true)
	// ram: the tenant has a rule and the tag has a tighter one. No device rule.
	f.rule(t, models.ScopeClient, f.client.ID, models.MetricRAM, 70, 85, 0, true)
	f.rule(t, models.ScopeTag, "prod", models.MetricRAM, 60, 75, 0, true)

	f.engine.Evaluate(metrics(62, 72, 88), f.device.ID, f.device.Name)

	cpu := f.lastAlertFor(t, f.device.ID, models.MetricCPU)
	if cpu.Severity != "warning" {
		t.Errorf("cpu severity = %q, want warning: the device rule is 50/65 and 62 sits above its warning only", cpu.Severity)
	}
	if cpu.Threshold != 50 {
		t.Errorf("cpu threshold = %v, want 50 (the device rule; the client's 70 and the tag's 60 are less specific)", cpu.Threshold)
	}
	if cpu.Message != "CPU usage elevated: 62.0% (threshold: 50%)" {
		t.Errorf("cpu message = %q, want the device rule named in it", cpu.Message)
	}

	ram := f.lastAlertFor(t, f.device.ID, models.MetricRAM)
	if ram.Threshold != 60 {
		t.Errorf("ram threshold = %v, want 60 (the tag rule beats the tenant's 70)", ram.Threshold)
	}
	if ram.Severity != "warning" {
		t.Errorf("ram severity = %q, want warning", ram.Severity)
	}
	if ram.Message != "RAM usage elevated: 72.0% (threshold: 60%)" {
		t.Errorf("ram message = %q, want the tag rule named in it", ram.Message)
	}

	// disk has no rule at all: the built-in floor, exactly as shipped.
	disk := f.lastAlertFor(t, f.device.ID, models.MetricDisk)
	if disk.Threshold != 85 || disk.Severity != "warning" {
		t.Errorf("disk = threshold %v severity %q, want 85 / warning from the built-in floor", disk.Threshold, disk.Severity)
	}
}

// The rule moved one unit. Its neighbour in the same tenant, with the same tag
// absent, keeps the tenant's number.
func TestDeviceRuleMovesOneUnitAndNotItsNeighbour(t *testing.T) {
	f := newFleet(t, []string{"prod"})
	f.rule(t, models.ScopeClient, f.client.ID, models.MetricCPU, 70, 85, 0, true)
	f.rule(t, models.ScopeTag, "prod", models.MetricCPU, 60, 75, 0, true)
	f.rule(t, models.ScopeDevice, f.device.ID, models.MetricCPU, 50, 65, 0, true)

	f.engine.Evaluate(metrics(62, 0, 0), f.device.ID, f.device.Name)
	f.engine.Evaluate(metrics(62, 0, 0), f.device2.ID, f.device2.Name)

	if got := f.lastAlertFor(t, f.device.ID, models.MetricCPU).Threshold; got != 50 {
		t.Errorf("the named unit's threshold = %v, want its own rule (50)", got)
	}
	// alpha-02 has no tags and no device rule, so its tenant's 70 applies — and
	// 62 is under it, so it raises nothing.
	for _, a := range f.alerts.alerts {
		if a.DeviceID == f.device2.ID && a.Metric == models.MetricCPU {
			t.Errorf("the neighbour unit alerted at threshold %v; its tenant rule warns at 70", a.Threshold)
		}
	}
}

// Two tag rules apply to the same unit: the tie-break must be the same answer
// every time, and it is the more sensitive rule.
func TestTieBreakBetweenTwoTagRulesIsTheMoreSensitiveRule(t *testing.T) {
	f := newFleet(t, []string{"prod", "eu"})
	f.rule(t, models.ScopeTag, "prod", models.MetricCPU, 60, 75, 0, true)
	f.rule(t, models.ScopeTag, "eu", models.MetricCPU, 55, 70, 0, true)

	f.engine.Evaluate(metrics(58, 0, 0), f.device.ID, f.device.Name)
	alert := f.lastAlertFor(t, f.device.ID, models.MetricCPU)
	if alert.Threshold != 55 {
		t.Errorf("threshold = %v, want 55: the tie between two tag rules breaks to the lowest warning", alert.Threshold)
	}
}

// A disabled rule is not a rule. The metric falls to the next most specific one.
func TestDisabledRuleIsNotApplied(t *testing.T) {
	// The unit carries no tag here: this test is about the rule rows, and a
	// tag rule that is the only match must be matched by a device that has it.
	f := newFleet(t, []string{"prod"})
	f.rule(t, models.ScopeTag, "prod", models.MetricCPU, 60, 75, 0, true)
	f.rule(t, models.ScopeDevice, f.device2.ID, models.MetricCPU, 50, 65, 0, false)

	// 85 clears the built-in floor's warning (80) and sits under the disabled
	// rule's 50, so the only answer that can produce this alert is the floor.
	f.engine.Evaluate(metrics(85, 0, 0), f.device2.ID, f.device2.Name)
	alert := f.lastAlertFor(t, f.device2.ID, models.MetricCPU)
	if alert.Threshold != 80 {
		t.Errorf("threshold = %v, want 80: the device rule is disabled and this unit matches no other rule, so the built-in floor applies", alert.Threshold)
	}

	// And the same unit, carrying the tag, falls through the disabled device
	// rule to the tag rule: a disabled rule is skipped, not treated as an answer.
	f.engine.Evaluate(metrics(62, 0, 0), f.device.ID, f.device.Name)
	if got := f.lastAlertFor(t, f.device.ID, models.MetricCPU).Threshold; got != 60 {
		t.Errorf("threshold = %v, want 60 (the disabled device rule must fall through to the tag rule)", got)
	}
}

// Auto-resolution measures the **resolved** warning. A unit whose rule says 50 is
// recovering at 48; the installation's 80 would keep its alert open forever.
func TestAutoResolveUsesTheResolvedWarning(t *testing.T) {
	f := newFleet(t, []string{})
	f.rule(t, models.ScopeDevice, f.device.ID, models.MetricCPU, 50, 65, 0, true)

	f.engine.Evaluate(metrics(70, 0, 0), f.device.ID, f.device.Name)
	alert := f.lastAlertFor(t, f.device.ID, models.MetricCPU)
	if alert.Severity != "critical" {
		t.Fatalf("severity = %q, want critical (70 is above this rule's 65)", alert.Severity)
	}
	if alert.ResolvedAt != nil {
		t.Fatal("the alert was resolved before the metric came down")
	}

	f.engine.Evaluate(metrics(48, 0, 0), f.device.ID, f.device.Name)
	after := f.lastAlertFor(t, f.device.ID, models.MetricCPU)
	if after.ResolvedAt == nil {
		t.Error("the critical alert stayed open at 48%: auto-resolution measured the global 80, not the unit's own 50")
	}
}

// The regression the release names: a device that no rule covers produces the
// `v2.3.x` alert byte for byte.
func TestDeviceWithNoRuleProducesTheShippedAlertExactly(t *testing.T) {
	f := newFleet(t, []string{"prod"})
	f.engine.Evaluate(metrics(85, 88, 96), f.device.ID, f.device.Name)

	type want struct {
		severity, message string
		threshold         float64
	}
	cases := map[string]want{
		models.MetricCPU:  {severity: "warning", threshold: 80, message: "CPU usage elevated: 85.0% (threshold: 80%)"},
		models.MetricRAM:  {severity: "warning", threshold: 85, message: "RAM usage elevated: 88.0% (threshold: 85%)"},
		models.MetricDisk: {severity: "critical", threshold: 95, message: "Disk usage high: 96.0% (threshold: 95%)"},
	}
	for metric, expected := range cases {
		alert := f.lastAlertFor(t, f.device.ID, metric)
		if alert.Severity != expected.severity {
			t.Errorf("%s severity = %q, want %q", metric, alert.Severity, expected.severity)
		}
		if alert.Threshold != expected.threshold {
			t.Errorf("%s threshold = %v, want %v", metric, alert.Threshold, expected.threshold)
		}
		if alert.Message != expected.message {
			t.Errorf("%s message = %q, want %q", metric, alert.Message, expected.message)
		}
	}
}

// The dedup window belongs to the rule: a unit that reports every minute and a
// remote unit that reports every ten minutes are not one alerting problem.
func TestRuleDedupWindowOverridesTheEngineDefault(t *testing.T) {
	f := newFleet(t, []string{})
	f.rule(t, models.ScopeDevice, f.device.ID, models.MetricCPU, 50, 60, 1, true)

	f.engine.Evaluate(metrics(55, 0, 0), f.device.ID, f.device.Name)
	if n := len(f.alerts.alerts); n != 1 {
		t.Fatalf("alerts = %d, want 1", n)
	}
	// The engine's own window is 5 minutes: the rule's 1 second is what lets the
	// second alert through.
	time.Sleep(1100 * time.Millisecond)
	f.engine.Evaluate(metrics(56, 0, 0), f.device.ID, f.device.Name)
	if n := len(f.alerts.alerts); n != 2 {
		t.Errorf("alerts = %d, want 2: the rule's 1s dedup window was not used (engine default is 5m)", n)
	}
}
