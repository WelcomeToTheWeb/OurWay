package patching

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

func newPolicyTestStore(t *testing.T) *store.Store {
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
	if !scheduleDue(" Weekly ", sunday) {
		t.Error("schedule matching should be case/whitespace normalized, not exact-match")
	}
	if scheduleDue("hourly", sunday) {
		t.Error("unknown schedules should never fire")
	}
}

func TestResolveScope(t *testing.T) {
	st := newPolicyTestStore(t)
	hub := ws.NewHub("http://localhost:3000", "")
	engine := NewPolicyEngine(st, hub, NewDeployer(st, hub), NewRebooter(hub), NewScanner(st, hub))

	// Three devices: two online, one offline.
	devices := []models.Device{
		{ID: "dev-1", Name: "laptop-1", Hostname: "laptop-1", OS: "linux", Arch: "amd64", Status: "online", DeviceKey: "key-1"},
		{ID: "dev-2", Name: "laptop-2", Hostname: "laptop-2", OS: "linux", Arch: "amd64", Status: "online", DeviceKey: "key-2"},
		{ID: "dev-3", Name: "server-1", Hostname: "server-1", OS: "windows", Arch: "amd64", Status: "offline", DeviceKey: "key-3"},
	}
	for i := range devices {
		if err := st.Devices.Create(&devices[i]); err != nil {
			t.Fatalf("failed to create device: %v", err)
		}
	}

	all, err := engine.resolveScope(&models.PatchPolicy{Scope: "all"})
	if err != nil {
		t.Fatalf("resolveScope all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("scope all should match 3 devices, got %d", len(all))
	}

	byIDs, err := engine.resolveScope(&models.PatchPolicy{Scope: "devices", ScopeValue: "dev-1, dev-3"})
	if err != nil {
		t.Fatalf("resolveScope devices: %v", err)
	}
	if len(byIDs) != 2 {
		t.Errorf("scope devices should match 2 devices, got %d (%v)", len(byIDs), byIDs)
	}

	byName, err := engine.resolveScope(&models.PatchPolicy{Scope: "devices", ScopeValue: "laptop-2"})
	if err != nil {
		t.Fatalf("resolveScope by name: %v", err)
	}
	if len(byName) != 1 || byName[0] != "dev-2" {
		t.Errorf("scope by device name should match dev-2, got %v", byName)
	}

	unknown, err := engine.resolveScope(&models.PatchPolicy{Scope: "tags", ScopeValue: "no-such-device"})
	if err != nil {
		t.Fatalf("resolveScope unknown: %v", err)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown scope value should match 0 devices, got %d", len(unknown))
	}

	bogus, err := engine.resolveScope(&models.PatchPolicy{Scope: "galaxy"})
	if err != nil {
		t.Fatalf("resolveScope bogus: %v", err)
	}
	if len(bogus) != 0 {
		t.Errorf("bogus scope should match 0 devices, got %d", len(bogus))
	}
}

func TestEvaluateDueAppliesPolicy(t *testing.T) {
	st := newPolicyTestStore(t)
	hub := ws.NewHub("http://localhost:3000", "")
	deployer := NewDeployer(st, hub)
	rebooter := NewRebooter(hub)
	scanner := NewScanner(st, hub)
	engine := NewPolicyEngine(st, hub, deployer, rebooter, scanner)

	device := &models.Device{ID: "dev-1", Name: "laptop-1", Hostname: "laptop-1", OS: "linux", Arch: "amd64", Status: "online", DeviceKey: "key-1"}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("failed to create device: %v", err)
	}
	// One detected update; the policy auto-approves it, then deploys.
	if err := st.SoftwareUpdates.Create(&models.SoftwareUpdate{
		ID: "upd-1", DeviceID: "dev-1", Source: "apt", Title: "curl security update", Status: "detected",
	}); err != nil {
		t.Fatalf("failed to create update: %v", err)
	}
	policy := &models.PatchPolicy{
		ID: "pol-1", Name: "weekly patch window", Scope: "all",
		Schedule: "weekly", AutoReboot: false, ApprovalRequired: false, MaxDevicesPerBatch: 10,
	}
	if err := st.PatchPolicies.Create(policy); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}

	// Sunday 03:00 — the weekly window is due.
	engine.Now = func() time.Time { return time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC) }
	engine.EvaluateDue(nil)

	// The detected update must have been auto-approved (the deploy phase
	// only targets approved updates).
	updates, err := st.SoftwareUpdates.ListByDevice("dev-1")
	if err != nil {
		t.Fatalf("failed to list updates: %v", err)
	}
	if len(updates) != 1 || updates[0].Status != "approved" {
		t.Fatalf("policy with ApprovalRequired=false should auto-approve the detected update, got %+v", updates)
	}

	// A deployment record must exist targeting the device.
	deployments, err := st.PatchDeployments.ListAll()
	if err != nil {
		t.Fatalf("failed to list deployments: %v", err)
	}
	if len(deployments) != 1 {
		t.Fatalf("policy run should create exactly 1 deployment, got %d", len(deployments))
	}
	if deployments[0].ID == "" {
		t.Error("deployment should have an ID")
	}
}

func TestEvaluateDueSkipsNonDueSchedule(t *testing.T) {
	st := newPolicyTestStore(t)
	hub := ws.NewHub("http://localhost:3000", "")
	engine := NewPolicyEngine(st, hub, NewDeployer(st, hub), NewRebooter(hub), NewScanner(st, hub))

	device := &models.Device{ID: "dev-1", Name: "laptop-1", Hostname: "laptop-1", OS: "linux", Arch: "amd64", Status: "online", DeviceKey: "key-1"}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("failed to create device: %v", err)
	}
	if err := st.SoftwareUpdates.Create(&models.SoftwareUpdate{
		ID: "upd-1", DeviceID: "dev-1", Source: "apt", Title: "curl security update", Status: "detected",
	}); err != nil {
		t.Fatalf("failed to create update: %v", err)
	}
	// Monthly policy evaluated mid-month: nothing should happen.
	if err := st.PatchPolicies.Create(&models.PatchPolicy{
		ID: "pol-1", Name: "monthly", Scope: "all", Schedule: "monthly", ApprovalRequired: false,
	}); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}
	engine.Now = func() time.Time { return time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC) }
	engine.EvaluateDue(nil)

	updates, err := st.SoftwareUpdates.ListByDevice("dev-1")
	if err != nil {
		t.Fatalf("failed to list updates: %v", err)
	}
	if updates[0].Status != "detected" {
		t.Errorf("non-due policy must not touch updates, status = %s", updates[0].Status)
	}
	deployments, err := st.PatchDeployments.ListAll()
	if err != nil {
		t.Fatalf("failed to list deployments: %v", err)
	}
	if len(deployments) != 0 {
		t.Errorf("non-due policy must not create deployments, got %d", len(deployments))
	}
}

func TestEvaluateDueRespectsApprovalRequired(t *testing.T) {
	st := newPolicyTestStore(t)
	hub := ws.NewHub("http://localhost:3000", "")
	engine := NewPolicyEngine(st, hub, NewDeployer(st, hub), NewRebooter(hub), NewScanner(st, hub))

	device := &models.Device{ID: "dev-1", Name: "laptop-1", Hostname: "laptop-1", OS: "linux", Arch: "amd64", Status: "online", DeviceKey: "key-1"}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("failed to create device: %v", err)
	}
	if err := st.SoftwareUpdates.Create(&models.SoftwareUpdate{
		ID: "upd-1", DeviceID: "dev-1", Source: "apt", Title: "curl security update", Status: "detected",
	}); err != nil {
		t.Fatalf("failed to create update: %v", err)
	}
	if err := st.PatchPolicies.Create(&models.PatchPolicy{
		ID: "pol-1", Name: "manual approval", Scope: "all", Schedule: "daily", ApprovalRequired: true,
	}); err != nil {
		t.Fatalf("failed to create policy: %v", err)
	}
	engine.Now = func() time.Time { return time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC) }
	engine.EvaluateDue(nil)

	updates, err := st.SoftwareUpdates.ListByDevice("dev-1")
	if err != nil {
		t.Fatalf("failed to list updates: %v", err)
	}
	if updates[0].Status != "detected" {
		t.Errorf("ApprovalRequired=true must not auto-approve, status = %s", updates[0].Status)
	}
	deployments, err := st.PatchDeployments.ListAll()
	if err != nil {
		t.Fatalf("failed to list deployments: %v", err)
	}
	if len(deployments) != 0 {
		t.Errorf("nothing approved means nothing to deploy, got %d deployments", len(deployments))
	}
}
