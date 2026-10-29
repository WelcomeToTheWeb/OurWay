package store

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/models"
)

// 2.4-5 · the resolver.
//
// "Most specific wins, per metric, independently" is a sentence with three ways
// to get it wrong: a rule for one metric leaking into another, a rule for one
// device reaching its neighbour, and a tie between two equally specific rules
// answering differently on Monday and Tuesday.

func newThresholdFixture(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	st, err := NewWithDB(db)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return st
}

func addRule(t *testing.T, st *Store, scopeType, scopeValue, metric string, warning, critical float64, enabled bool) *models.AlertThreshold {
	t.Helper()
	row := &models.AlertThreshold{
		ScopeType: scopeType, ScopeValue: scopeValue, Metric: metric,
		Warning: warning, Critical: critical, Enabled: enabled,
	}
	if err := st.Thresholds.Create(row); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	return row
}

func TestResolveTakesTheMostSpecificRulePerMetric(t *testing.T) {
	st := newThresholdFixture(t)
	const device, client = "device-1", "client-1"

	addRule(t, st, models.ScopeGlobal, "", models.MetricCPU, 80, 90, true)
	addRule(t, st, models.ScopeClient, client, models.MetricCPU, 70, 85, true)
	addRule(t, st, models.ScopeTag, "prod", models.MetricCPU, 60, 75, true)
	addRule(t, st, models.ScopeDevice, device, models.MetricCPU, 50, 65, true)
	// ram: only the tenant names it.
	addRule(t, st, models.ScopeClient, client, models.MetricRAM, 70, 85, true)

	got, err := st.Thresholds.Resolve(device, []string{"prod"}, client, models.MetricCPU)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Warning != 50 || got.Critical != 65 || got.ScopeType != models.ScopeDevice {
		t.Errorf("cpu = %+v, want the device rule 50/65", got)
	}
	if got.Source != "rule" {
		t.Errorf("source = %q, want rule", got.Source)
	}

	// Each metric is resolved on its own: ram has no device rule, so the
	// tenant's answer applies to the same unit.
	ram, err := st.Thresholds.Resolve(device, []string{"prod"}, client, models.MetricRAM)
	if err != nil {
		t.Fatalf("resolve ram: %v", err)
	}
	if ram.Warning != 70 || ram.ScopeType != models.ScopeClient {
		t.Errorf("ram = %+v, want the client rule 70/85", ram)
	}

	// disk has no rule at all: the built-in floor, stated as the source.
	disk, err := st.Thresholds.Resolve(device, []string{"prod"}, client, models.MetricDisk)
	if err != nil {
		t.Fatalf("resolve disk: %v", err)
	}
	if disk.Warning != 85 || disk.Critical != 95 || disk.Source != "built-in default" {
		t.Errorf("disk = %+v, want the built-in floor 85/95", disk)
	}

	// A neighbour in the same tenant keeps the tenant's number: a device rule is
	// a rule about one unit.
	other, err := st.Thresholds.Resolve("device-2", []string{"prod"}, client, models.MetricCPU)
	if err != nil {
		t.Fatalf("resolve neighbour: %v", err)
	}
	if other.Warning != 60 || other.ScopeType != models.ScopeTag {
		t.Errorf("neighbour cpu = %+v, want the tag rule 60/75", other)
	}
}

func TestResolveSkipsDisabledRules(t *testing.T) {
	st := newThresholdFixture(t)
	addRule(t, st, models.ScopeTag, "prod", models.MetricCPU, 60, 75, true)
	addRule(t, st, models.ScopeDevice, "device-1", models.MetricCPU, 50, 65, false)

	got, err := st.Thresholds.Resolve("device-1", []string{"prod"}, "client-1", models.MetricCPU)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Warning != 60 || got.ScopeType != models.ScopeTag {
		t.Errorf("cpu = %+v, want the tag rule: a disabled rule must fall through, not answer", got)
	}
}

// A rule created disabled stays disabled. GORM substitutes a column default for a
// zero-valued field on insert, so `default:true` on this column turned
// "disable this rule" into "keep firing it".
func TestDisabledRuleIsStoredDisabled(t *testing.T) {
	st := newThresholdFixture(t)
	row := addRule(t, st, models.ScopeDevice, "device-1", models.MetricCPU, 50, 65, false)

	var stored models.AlertThreshold
	if err := st.DB.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.Enabled {
		t.Error("a rule created with Enabled=false was stored enabled")
	}
}

func TestResolveIgnoresClientRuleForAnUnassignedUnit(t *testing.T) {
	st := newThresholdFixture(t)
	addRule(t, st, models.ScopeClient, "client-1", models.MetricCPU, 70, 85, true)

	// An unassigned unit has no tenant: the tenant's rule cannot reach it.
	got, err := st.Thresholds.Resolve("device-loose", nil, "", models.MetricCPU)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Source != "built-in default" {
		t.Errorf("unassigned unit = %+v, want the built-in floor", got)
	}
}

// Tag rules compare normalized: the rule is typed as "Prod", the device row
// stores "prod", and a rule that silently never matches is worse than no rule.
func TestTagRuleMatchesTheNormalizedTag(t *testing.T) {
	st := newThresholdFixture(t)
	addRule(t, st, models.ScopeTag, "Prod", models.MetricCPU, 60, 75, true)

	got, err := st.Thresholds.Resolve("device-1", []string{"prod"}, "client-1", models.MetricCPU)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ScopeType != models.ScopeTag || got.Warning != 60 {
		t.Errorf("cpu = %+v, want the tag rule to match the device's lowercase tag", got)
	}
}

// Two tag rules on the same metric: the tie-break must be a rule, not a row order.
func TestTieBreakBetweenEqualScopeRulesIsDeterministic(t *testing.T) {
	st := newThresholdFixture(t)
	addRule(t, st, models.ScopeTag, "eu", models.MetricCPU, 55, 70, true)
	addRule(t, st, models.ScopeTag, "prod", models.MetricCPU, 60, 75, true)

	first, err := st.Thresholds.Resolve("device-1", []string{"prod", "eu"}, "client-1", models.MetricCPU)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if first.Warning != 55 {
		t.Errorf("warning = %v, want 55 (lowest warning wins the tie)", first.Warning)
	}

	// Seeded in the other order, same answer.
	st2 := newThresholdFixture(t)
	addRule(t, st2, models.ScopeTag, "prod", models.MetricCPU, 60, 75, true)
	addRule(t, st2, models.ScopeTag, "eu", models.MetricCPU, 55, 70, true)
	second, err := st2.Thresholds.Resolve("device-1", []string{"eu", "prod"}, "client-1", models.MetricCPU)
	if err != nil {
		t.Fatalf("resolve second: %v", err)
	}
	if second.Warning != first.Warning || second.ScopeValue != first.ScopeValue {
		t.Errorf("the tie-break depends on insertion order: %v/%v vs %v/%v",
			second.ScopeValue, second.Warning, first.ScopeValue, first.Warning)
	}
}

// The rule read is a cache read (a fleet posts metrics continuously). A write
// through this process invalidates it; a write from elsewhere lands within the
// TTL, and `Invalidate` is the immediate door.
func TestRuleSnapshotRefreshesOnInvalidate(t *testing.T) {
	st := newThresholdFixture(t)
	addRule(t, st, models.ScopeDevice, "device-1", models.MetricCPU, 50, 65, true)

	got, err := st.Thresholds.Resolve("device-1", nil, "", models.MetricCPU)
	if err != nil || got.Warning != 50 {
		t.Fatalf("resolve = %+v err %v, want the seeded rule", got, err)
	}

	// A write that bypasses the store (another process, a repair script).
	if err := st.DB.Model(&models.AlertThreshold{}).Where("metric = ?", "cpu").
		Update("warning", 33).Error; err != nil {
		t.Fatalf("external write: %v", err)
	}
	if cached, err := st.Thresholds.Resolve("device-1", nil, "", models.MetricCPU); err != nil || cached.Warning != 50 {
		t.Errorf("the snapshot was not a cache: %+v err %v", cached, err)
	}
	st.Thresholds.Invalidate()
	fresh, err := st.Thresholds.Resolve("device-1", nil, "", models.MetricCPU)
	if err != nil {
		t.Fatalf("resolve after invalidate: %v", err)
	}
	if fresh.Warning != 33 {
		t.Errorf("warning = %v after Invalidate, want the stored 33", fresh.Warning)
	}
}

// A store write must invalidate on its own: a rule the operator just saved is the
// rule the next metric is measured against, not the rule from 15 seconds ago.
func TestCreateAndDeleteInvalidateTheSnapshot(t *testing.T) {
	st := newThresholdFixture(t)
	row := addRule(t, st, models.ScopeDevice, "device-1", models.MetricCPU, 50, 65, true)

	if err := st.DB.Model(&models.AlertThreshold{}).Where("id = ?", row.ID).Update("warning", 20).Error; err != nil {
		t.Fatalf("external write: %v", err)
	}
	if err := st.Thresholds.Delete(row.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	fresh, err := st.Thresholds.Resolve("device-1", nil, "", models.MetricCPU)
	if err != nil {
		t.Fatalf("resolve after delete: %v", err)
	}
	if fresh.Source != "built-in default" {
		t.Errorf("after deleting the rule, resolve = %+v, want the built-in floor", fresh)
	}
}
