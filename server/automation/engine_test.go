package automation

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return st
}

func TestScheduleDue(t *testing.T) {
	sunday := time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC)
	first := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	if !scheduleDue("daily", sunday) {
		t.Error("daily schedule should be due every day")
	}
	if !scheduleDue("weekly", sunday) {
		t.Error("weekly schedule should be due on Sunday")
	}
	if scheduleDue("weekly", monday) {
		t.Error("weekly schedule should not be due on Monday")
	}
	if !scheduleDue("monthly", first) {
		t.Error("monthly schedule should be due on the 1st")
	}
	if scheduleDue("monthly", monday) {
		t.Error("monthly schedule should not be due mid-month")
	}
	if scheduleDue("hourly", sunday) {
		t.Error("unknown schedules should never be due")
	}
}

func TestRanThisWindow(t *testing.T) {
	rb := &models.Runbook{Schedule: "daily", WindowStart: "02:00", WindowHours: 4}
	now := time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)
	_, _, in := inWindow(rb, now)
	if !in {
		t.Fatal("03:00 should be inside a 02:00+4h window")
	}
	// Not run yet.
	if _, _, already := ranThisWindow(rb, now); already {
		t.Error("runbook with no LastRunAt should not count as already run")
	}
	// Run earlier in the same window.
	ran := now.Add(-30 * time.Minute)
	rb.LastRunAt = &ran
	if _, _, already := ranThisWindow(rb, now); !already {
		t.Error("LastRunAt inside the current window should suppress a re-run")
	}
	// Run in a previous window (yesterday's window ends 06:00 on the 3rd).
	old := time.Date(2026, 10, 3, 5, 0, 0, 0, time.UTC)
	rb.LastRunAt = &old
	if _, _, already := ranThisWindow(rb, now); already {
		t.Error("LastRunAt outside the current window should not suppress a run")
	}
}

func TestResolveScope(t *testing.T) {
	st := newTestStore(t)
	engine := NewEngine(st, ws.NewHub("http://localhost:3000", ""))

	devs := []models.Device{
		{ID: "d1", Name: "laptop", Hostname: "laptop", OS: "linux", Arch: "amd64", DeviceKey: "k1", Tags: []string{"prod"}},
		{ID: "d2", Name: "desktop", Hostname: "desktop", OS: "linux", Arch: "amd64", DeviceKey: "k2", Tags: []string{"dev"}},
	}
	for i := range devs {
		if err := st.Devices.Create(&devs[i]); err != nil {
			t.Fatalf("create device: %v", err)
		}
	}

	all, err := engine.resolveScope(&models.Runbook{Scope: "all"})
	if err != nil || len(all) != 2 {
		t.Errorf("scope all: %v %d", err, len(all))
	}
	byTag, err := engine.resolveScope(&models.Runbook{Scope: "tags", ScopeValue: "prod"})
	if err != nil || len(byTag) != 1 || byTag[0] != "d1" {
		t.Errorf("scope tags: %v %v", err, byTag)
	}
	byName, err := engine.resolveScope(&models.Runbook{Scope: "devices", ScopeValue: "desktop"})
	if err != nil || len(byName) != 1 || byName[0] != "d2" {
		t.Errorf("scope devices by name: %v %v", err, byName)
	}
	none, err := engine.resolveScope(&models.Runbook{Scope: "bogus"})
	if err != nil || len(none) != 0 {
		t.Errorf("unknown scope should resolve to no devices: %v %v", err, none)
	}
}

func TestEvaluateDueSkipsDisabledAndOffSchedule(t *testing.T) {
	st := newTestStore(t)
	engine := NewEngine(st, ws.NewHub("http://localhost:3000", ""))
	// A Monday: weekly runbooks are not due.
	engine.Now = func() time.Time { return time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC) }

	rb := &models.Runbook{ID: "rb1", Name: "cleanup", Scope: "all", Schedule: "weekly", Command: "true", Enabled: true}
	if err := st.Runbooks.Create(rb); err != nil {
		t.Fatalf("create runbook: %v", err)
	}
	engine.EvaluateDue()
	got, err := st.Runbooks.GetByID("rb1")
	if err != nil {
		t.Fatalf("get runbook: %v", err)
	}
	if got.LastRunAt != nil {
		t.Error("weekly runbook should not run on Monday")
	}

	// Disabled runbooks never run, even when due.
	engine.Now = func() time.Time { return time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC) } // Sunday
	rb2 := &models.Runbook{ID: "rb2", Name: "disabled", Scope: "all", Schedule: "weekly", Command: "true", Enabled: false}
	if err := st.Runbooks.Create(rb2); err != nil {
		t.Fatalf("create runbook: %v", err)
	}
	engine.EvaluateDue()
	got2, _ := st.Runbooks.GetByID("rb2")
	if got2.LastRunAt != nil {
		t.Error("disabled runbook should not run")
	}
	got1, _ := st.Runbooks.GetByID("rb1")
	if got1.LastRunAt == nil {
		t.Fatal("enabled weekly runbook should run on Sunday")
	}

	// A second evaluation in the same window must not re-run it.
	first := *got1.LastRunAt
	engine.EvaluateDue()
	got1, _ = st.Runbooks.GetByID("rb1")
	if !got1.LastRunAt.Equal(first) {
		t.Error("runbook must not run twice in the same window")
	}
}

func TestDispatchRecordsFailedRunForUnreachableDevice(t *testing.T) {
	st := newTestStore(t)
	// No device: SendToDevice fails and the run must be recorded as failed.
	dev := &models.Device{ID: "d1", Name: "laptop", Hostname: "laptop", OS: "linux", Arch: "amd64", DeviceKey: "k1", Status: "online"}
	if err := st.Devices.Create(dev); err != nil {
		t.Fatalf("create device: %v", err)
	}
	engine := NewEngine(st, ws.NewHub("http://localhost:3000", ""))
	rb := &models.Runbook{ID: "rb1", Name: "cleanup", Scope: "all", Command: "true", Enabled: true}
	if err := st.Runbooks.Create(rb); err != nil {
		t.Fatalf("create runbook: %v", err)
	}
	engine.DispatchNow(rb)
	runs, err := st.Runbooks.ListRunsByRunbook("rb1", 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("expected one run row: %v %d", err, len(runs))
	}
	if runs[0].Status != "failed" {
		t.Errorf("unreachable device should record a failed run, got %q", runs[0].Status)
	}
	if runs[0].FinishedAt == nil {
		t.Error("failed run should be finished")
	}
}
