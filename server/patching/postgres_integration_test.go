package patching

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// Exercises the patch pipeline against a real PostgreSQL (uuid columns and
// DDL behave differently from SQLite). Set OURWAY_TEST_PG_DSN to run, e.g.
//   OURWAY_TEST_PG_DSN="host=/tmp user=me dbname=ourway_test sslmode=disable"
func pgPatchStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("OURWAY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("OURWAY_TEST_PG_DSN not set")
	}
	st, err := store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{"DROP SCHEMA public CASCADE", "CREATE SCHEMA public"} {
		if err := st.DB.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	st, err = store.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestPostgresPolicyRunDeployAndQueue(t *testing.T) {
	st := pgPatchStore(t)
	hub := ws.NewHub("http://localhost:3000", "")
	e := NewPolicyEngine(st, hub, NewDeployer(st, hub), NewRebooter(hub).WithStore(st), NewScanner(st, hub))
	e.ScanWait = 0

	online := &models.Device{ID: uuid.NewString(), Name: "on", Hostname: "on", OS: "linux", Arch: "amd64", Status: "online", DeviceKey: "k-" + uuid.NewString()}
	offline := &models.Device{ID: uuid.NewString(), Name: "off", Hostname: "off", OS: "windows", Arch: "amd64", Status: "offline", DeviceKey: "k-" + uuid.NewString()}
	for _, d := range []*models.Device{online, offline} {
		if err := st.Devices.Create(d); err != nil {
			t.Fatalf("device: %v", err)
		}
	}
	for _, d := range []*models.Device{online, offline} {
		if err := st.SoftwareUpdates.UpsertDetected(&models.SoftwareUpdate{ID: uuid.NewString(), DeviceID: d.ID, Source: "apt", Title: "curl", Version: "1"}); err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	policy := &models.PatchPolicy{ID: uuid.NewString(), Name: "nightly", Scope: "all", Schedule: "daily",
		WindowStart: "02:00", WindowHours: 4, Timezone: "UTC", ApprovalRequired: false, MaxDevicesPerBatch: 10}
	if err := st.PatchPolicies.Create(policy); err != nil {
		t.Fatalf("policy: %v", err)
	}
	if _, err := st.PatchPolicies.GetByID(policy.ID); err != nil {
		t.Fatalf("policy must be stored under its own ID: %v", err)
	}

	e.Now = func() time.Time { return time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC) }
	e.EvaluateDue(nil)

	deps, err := st.PatchDeployments.ListAll()
	if err != nil || len(deps) != 1 {
		t.Fatalf("expected one deployment, got %d (%v)", len(deps), err)
	}
	if deps[0].PolicyID == nil || *deps[0].PolicyID != policy.ID {
		t.Fatalf("deployment should link its policy, got %v", deps[0].PolicyID)
	}
	q, err := st.QueuedDeploys.ListActive(e.Now())
	// The offline device is queued up front; the "online" one too, because
	// this test hub has no live connection so its deploy command could not
	// be delivered.
	queued := map[string]bool{}
	for _, entry := range q {
		queued[entry.DeviceID] = true
	}
	if err != nil || !queued[offline.ID] || !queued[online.ID] {
		t.Fatalf("both undeliverable devices should be queued: %v %+v", err, q)
	}
	// Manual deploy (no policy) must work too.
	if _, err := NewDeployer(st, hub).DeployToDevices(nil, []string{online.ID}); err != nil && err != ErrNoApprovedUpdates {
		t.Fatalf("manual deploy: %v", err)
	}
}
