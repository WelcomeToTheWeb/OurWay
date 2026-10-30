package metrics

import (
	"os"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/models"
	"ourway/server/recordings"
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

// TestRetentionPrunesAuditLogOnTheSameSweep: 2.3-6's retention rule is that
// the ledger is pruned by the manager that already owns this job, on the same
// tick, with a counter. A second goroutine nobody supervises is how a retention
// policy quietly stops happening (M11), so the test asserts both halves: the
// rows past the window are gone, and the sweep says how many went.
func TestRetentionPrunesAuditLogOnTheSameSweep(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.MetricHistory{}, &models.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })

	metrics := store.NewMetricHistoryStore(db)
	audit := store.NewAuditStore(db)
	now := time.Now()
	seed := []models.AuditLog{
		{ID: "00000000-0000-0000-0000-000000000001", Action: "device.delete", Outcome: "ok", CreatedAt: now.Add(-120 * 24 * time.Hour)},
		{ID: "00000000-0000-0000-0000-000000000002", Action: "device.delete", Outcome: "ok", CreatedAt: now.Add(-95 * 24 * time.Hour)},
		{ID: "00000000-0000-0000-0000-000000000003", Action: "patch.deploy", Outcome: "refused", CreatedAt: now.Add(-30 * 24 * time.Hour)},
		{ID: "00000000-0000-0000-0000-000000000004", Action: "patch.deploy", Outcome: "ok", CreatedAt: now.Add(-time.Hour)},
	}
	for i := range seed {
		if err := audit.Record(&seed[i]); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	manager := NewRetentionManager(metrics, DefaultRetentionPolicy(), time.Hour).
		WithAudit(AuditRetentionPolicy{Store: audit, Window: 90 * 24 * time.Hour})
	manager.enforce()

	if n := audit.Count(); n != 2 {
		t.Errorf("ledger holds %d rows, want the 2 inside the 90-day window", n)
	}
	counters := manager.Counters()
	if counters.AuditDeleted != 2 {
		t.Errorf("counter = %d pruned rows, want 2: the sweep must be observable", counters.AuditDeleted)
	}
	if counters.RetentionDays != 90 {
		t.Errorf("window = %d days, want the configured 90", counters.RetentionDays)
	}
	if counters.LastRunAt.IsZero() {
		t.Error("the sweep recorded no timestamp")
	}
	if counters.Errored {
		t.Error("the sweep reported an error")
	}

	// The metric tiers keep running beside the ledger's prune, in the same tick.
	if counters.MetricDeleted < 0 {
		t.Errorf("metric counter = %d, want a real count", counters.MetricDeleted)
	}

	// The ledger's page reads the same counters, not a second source.
	pruned, ranAt, errored := manager.RetentionSummary()
	if pruned != 2 || ranAt.IsZero() || errored {
		t.Errorf("RetentionSummary = %d/%v/%v, want 2 and a real run", pruned, ranAt, errored)
	}
}

// TestRetentionSurvivesAuditFailure: a ledger that cannot be pruned must not
// stop the metric cleanup. The two tables are independent, and a sweeper that
// aborts on the first error turns a read failure into unbounded growth.
func TestRetentionSurvivesAuditFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.MetricHistory{}); err != nil {
		t.Fatalf("migrate metrics: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })

	metrics := store.NewMetricHistoryStore(db)
	old := []models.MetricHistory{
		{DeviceID: "d1", Timestamp: time.Now().Add(-400 * 24 * time.Hour), CPU: 1},
	}
	if err := metrics.BatchInsert(old); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The audit store points at a database with no audit_log table: the prune
	// cannot succeed, and the sweep must report that rather than stop.
	manager := NewRetentionManager(metrics, DefaultRetentionPolicy(), time.Hour).
		WithAudit(AuditRetentionPolicy{Store: store.NewAuditStore(db), Window: 90 * 24 * time.Hour})
	manager.enforce()

	counters := manager.Counters()
	if counters.MetricDeleted != 1 {
		t.Errorf("metric sweep deleted %d rows, want 1: the audit failure must not abort it", counters.MetricDeleted)
	}
	if !counters.Errored {
		t.Error("the sweep must report the audit prune's failure")
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

// TestRetentionSweepsRecordingsOnTheSameTick: 2.4-8's recordings are a second
// table with a retention window, so they get 2.3-6's treatment — the manager that
// already owns this job, the same tick, a counter an operator can read. A
// recording nobody prunes is a disk that fills up quietly, which is M11's pattern.
func TestRetentionSweepsRecordingsOnTheSameTick(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.MetricHistory{}, &models.AuditLog{}, &models.Session{}, &models.SessionRecording{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })

	metrics := store.NewMetricHistoryStore(db)
	stores, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	policy := recordings.Policy{Mode: models.RecordingModeOn, Dir: t.TempDir(), Retention: 30 * 24 * time.Hour}
	policy.Normalize()
	writer := recordings.NewWriter(policy)
	t.Cleanup(writer.Close)

	now := time.Now().UTC()

	// A recording whose window closed, and a recording inside the window.
	expired := &models.SessionRecording{
		ID: "expired-recording", SessionID: "session-expired", DeviceID: "device-1", UserID: "user-1",
		StartedAt: now.Add(-40 * 24 * time.Hour), FrameCount: 3, Path: policy.PathFor("expired-recording"),
		Policy: models.RecordingModeOn, Reason: models.RecordingReasonPolicyOn,
	}
	inside := &models.SessionRecording{
		ID: "inside-recording", SessionID: "session-inside", DeviceID: "device-1", UserID: "user-1",
		StartedAt: now.Add(-24 * time.Hour), FrameCount: 3, Path: policy.PathFor("inside-recording"),
		Policy: models.RecordingModeOn, Reason: models.RecordingReasonPolicyOn,
	}
	insideUntil := now.Add(29 * 24 * time.Hour)
	inside.RetentionUntil = &insideUntil
	expired.RetentionUntil = &now
	for _, rec := range []*models.SessionRecording{expired, inside} {
		if err := stores.Recordings.Create(rec); err != nil {
			t.Fatalf("create recording: %v", err)
		}
		if err := writer.Start(rec); err != nil {
			t.Fatalf("start: %v", err)
		}
		if err := writer.Append(rec.ID, []byte("frame"), rec.StartedAt); err != nil {
			t.Fatalf("append: %v", err)
		}
		if _, _, err := writer.Finish(rec.ID); err != nil {
			t.Fatalf("finish: %v", err)
		}
	}

	manager := NewRetentionManager(metrics, DefaultRetentionPolicy(), time.Hour).
		WithRecordings(RecordingRetentionPolicy{Sweep: recordings.NewSweep(stores, writer, 30*24*time.Hour)})
	manager.enforce()

	counters := manager.Counters()
	if counters.RecordingsDeleted != 1 {
		t.Errorf("counter = %d recordings pruned, want 1", counters.RecordingsDeleted)
	}
	if counters.RecordingFiles != 1 {
		t.Errorf("counter = %d files removed, want 1", counters.RecordingFiles)
	}
	if _, err := stores.Recordings.GetByID(expired.ID); err == nil {
		t.Error("the expired recording survived the sweep")
	}
	if _, err := os.Stat(expired.Path); !os.IsNotExist(err) {
		t.Error("the expired recording's file survived the sweep")
	}
	if _, err := stores.Recordings.GetByID(inside.ID); err != nil {
		t.Error("a recording inside the window was pruned")
	}
	if !recordings.Exists(inside.Path) {
		t.Error("a recording inside the window lost its file")
	}
}
