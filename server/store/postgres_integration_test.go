package store

import (
	"os"
	"testing"

	"github.com/google/uuid"

	"ourway/server/models"
)

// Runs against a real PostgreSQL when OURWAY_TEST_PG_DSN is set, e.g.
//
//	OURWAY_TEST_PG_DSN="host=/tmp user=me dbname=ourway_test sslmode=disable"
//
// SQLite is lenient about uuid columns and DDL; production is Postgres.
func pgStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("OURWAY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("OURWAY_TEST_PG_DSN not set")
	}
	st, err := New(dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, stmt := range []string{"DROP SCHEMA public CASCADE", "CREATE SCHEMA public"} {
		if err := st.DB.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	st, err = New(dsn)
	if err != nil {
		t.Fatalf("New after reset: %v", err)
	}
	return st
}

func TestPostgresPatchLifecycle(t *testing.T) {
	st := pgStore(t)
	dev := &models.Device{ID: uuid.NewString(), Name: "w1", Hostname: "w1", OS: "windows", Arch: "amd64", DeviceKey: "k-" + uuid.NewString(), Tags: []string{"prod"}}
	if err := st.Devices.Create(dev); err != nil {
		t.Fatalf("create device: %v", err)
	}
	mk := func() *models.SoftwareUpdate {
		return &models.SoftwareUpdate{ID: uuid.NewString(), DeviceID: dev.ID, Source: "windows-update", Title: "KB1", ExternalID: "e1", Severity: "critical"}
	}
	if err := st.SoftwareUpdates.UpsertDetected(mk()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := st.SoftwareUpdates.UpsertDetected(mk()); err != nil {
		t.Fatalf("upsert again: %v", err)
	}
	if ups, _ := st.SoftwareUpdates.ListByDevice(dev.ID); len(ups) != 1 {
		t.Fatalf("want 1 update, got %d", len(ups))
	}
	got, err := st.Devices.GetByID(dev.ID)
	if err != nil || len(got.Tags) != 1 || got.Tags[0] != "prod" {
		t.Fatalf("tags round trip: %v %+v", err, got)
	}

	// Manual deployments have no policy.
	dep := &models.PatchDeployment{ID: uuid.NewString(), Status: "pending", DeviceIDs: []string{dev.ID}}
	if err := st.PatchDeployments.Create(dep); err != nil {
		t.Fatalf("create deployment without a policy: %v", err)
	}
}

func TestPostgresMigrationsUpgradeFromV1(t *testing.T) {
	st := pgStore(t)
	// Simulate a database created before migrations 2+ shipped.
	for _, stmt := range []string{
		"ALTER TABLE sessions DROP COLUMN remote_token_hash",
		"ALTER TABLE software_updates DROP COLUMN external_id, DROP COLUMN kb, DROP COLUMN severity, DROP COLUMN category, DROP COLUMN deployment_id",
		"ALTER TABLE devices DROP COLUMN reboot_pending, DROP COLUMN tags",
		"DELETE FROM schema_migrations WHERE version > 1",
	} {
		if err := st.DB.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := Migrate(st.DB); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	versions, _ := AppliedVersions(st.DB)
	if len(versions) != len(migrations) {
		t.Fatalf("applied %v, want %d", versions, len(migrations))
	}
}
