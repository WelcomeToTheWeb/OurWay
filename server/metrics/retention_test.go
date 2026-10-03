package metrics

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/models"
	"ourway/server/store"
)

func newTestMetricStore(t *testing.T) *store.MetricHistoryStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory SQLite: %v", err)
	}
	if err := db.AutoMigrate(&models.MetricHistory{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	return store.NewMetricHistoryStore(db)
}

func TestRetentionEnforceDeletesOnlyExpiredMetrics(t *testing.T) {
	st := newTestMetricStore(t)
	now := time.Now()
	records := []models.MetricHistory{
		{DeviceID: "d1", Timestamp: now.Add(-2 * 365 * 24 * time.Hour), CPU: 1}, // older than the cold cutoff: deleted
		{DeviceID: "d1", Timestamp: now.Add(-400 * 24 * time.Hour), CPU: 2},     // older than 365d: deleted
		{DeviceID: "d1", Timestamp: now.Add(-100 * 24 * time.Hour), CPU: 3},     // inside the cold window: kept
		{DeviceID: "d1", Timestamp: now.Add(-time.Hour), CPU: 4},                // kept
	}
	if err := st.BatchInsert(records); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	m := NewRetentionManager(st, DefaultRetentionPolicy(), time.Hour)
	m.enforce()

	count, err := st.TotalCount()
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 records to survive retention, got %d", count)
	}
	// The surviving records must be the recent ones.
	latest, err := st.QueryLatestByDevice("d1")
	if err != nil {
		t.Fatalf("latest query failed: %v", err)
	}
	if latest.CPU != 4 {
		t.Errorf("expected latest surviving record CPU=4, got %v", latest.CPU)
	}
}

func TestDefaultRetentionPolicyWindows(t *testing.T) {
	p := DefaultRetentionPolicy()
	if p.Hot != 7*24*time.Hour {
		t.Errorf("hot retention = %v, want 7d", p.Hot)
	}
	if p.Warm != 30*24*time.Hour {
		t.Errorf("warm retention = %v, want 30d", p.Warm)
	}
	if p.Cold != 365*24*time.Hour {
		t.Errorf("cold retention = %v, want 365d", p.Cold)
	}
}
