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

// 2.3-0's acceptance, on the database the release actually upgrades: Postgres.
// SQLite builds a fresh schema every time and so cannot show that a 2.2 row
// survives the column addition.
//
// The claim being tested is the one the upgrade guide has to keep: a 2.2
// database comes forward with every device intact, every device readable, and
// every device *unassigned* — because `client_id IS NULL` is the whole
// definition of unassigned, and no migration invents a tenant for anybody.
func TestPostgresMigrationsUpgradeFromV22(t *testing.T) {
	st := pgStore(t)

	// A fleet as 2.2 left it: devices, no client column, no client tables.
	// Created through the store, so the rows are the shape the product writes.
	devices := []string{}
	for i := 0; i < 3; i++ {
		d := &models.Device{
			ID:        uuid.NewString(),
			Name:      "unit-0" + string(rune('1'+i)),
			Hostname:  "unit-0" + string(rune('1'+i)),
			OS:        "linux",
			Arch:      "amd64",
			Status:    "online",
			Tags:      []string{},
			DeviceKey: uuid.NewString(),
		}
		if err := st.Devices.Create(d); err != nil {
			t.Fatalf("create device: %v", err)
		}
		devices = append(devices, d.ID)
	}

	// Roll the schema back to 2.2: drop what 2.3 adds, and forget it was ever
	// applied. Re-running Migrate must put it back, idempotently.
	for _, stmt := range []string{
		"ALTER TABLE devices DROP COLUMN client_id",
		"DROP TABLE IF EXISTS user_clients",
		"DROP TABLE IF EXISTS clients",
		"DELETE FROM schema_migrations WHERE version > 7",
	} {
		if err := st.DB.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if err := Migrate(st.DB); err != nil {
		t.Fatalf("upgrade 2.2 → 2.3: %v", err)
	}
	if err := Migrate(st.DB); err != nil {
		t.Fatalf("second Migrate (idempotency): %v", err)
	}
	versions, _ := AppliedVersions(st.DB)
	if len(versions) != len(migrations) {
		t.Fatalf("applied %v, want %d", versions, len(migrations))
	}

	// Every device is still there and readable, with no client.
	for _, id := range devices {
		d, err := st.Devices.GetByID(id)
		if err != nil {
			t.Fatalf("device %s after upgrade: %v", id, err)
		}
		if d == nil {
			t.Fatalf("device %s vanished in the upgrade", id)
		}
		if d.ClientID != nil {
			t.Fatalf("device %s came forward with client %v; the upgrade assigns nothing", id, *d.ClientID)
		}
	}

	// The unassigned bucket is exactly that fleet — the sentinel the API
	// presents, computed from NULLs, with no row behind it.
	unassigned, err := st.Devices.ListScoped(nil, true)
	if err != nil {
		t.Fatalf("ListScoped(unassigned): %v", err)
	}
	if len(unassigned) != len(devices) {
		t.Fatalf("unassigned bucket has %d devices, want %d", len(unassigned), len(devices))
	}

	// The client filter is indexed: 2.3-8's rule is that scoping must not cost
	// a scan per list, and the index is the part a migration can forget.
	var indexed bool
	if err := st.DB.Raw(
		"SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = 'devices' AND indexdef ILIKE '%client_id%')",
	).Row().Scan(&indexed); err != nil {
		t.Fatalf("index probe: %v", err)
	}
	if !indexed {
		t.Fatal("devices.client_id has no index after the upgrade")
	}

	// The plan names `client_id` a foreign key. On Postgres that is enforced,
	// which is the difference between "the store is careful" and "a device
	// cannot point at a client that does not exist".
	var fk bool
	if err := st.DB.Raw(
		"SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_devices_client')",
	).Row().Scan(&fk); err != nil {
		t.Fatalf("fk probe: %v", err)
	}
	if !fk {
		t.Fatal("devices.client_id has no foreign key to clients after the upgrade")
	}

	// And the FK does not obstruct the deletion rule: removing a client moves
	// its devices to unassigned, it does not fail on the reference.
	client := &models.Client{ID: models.NewClientID(), Name: "Acme", Slug: "acme"}
	if _, err := st.Clients.Create(client); err != nil {
		t.Fatalf("create client: %v", err)
	}
	if _, err := st.Clients.AssignDevices(client.ID, devices); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := st.Clients.Delete(client.ID); err != nil {
		t.Fatalf("delete client with devices: %v", err)
	}
	after, err := st.Devices.ListScoped(nil, true)
	if err != nil {
		t.Fatalf("ListScoped: %v", err)
	}
	if len(after) != len(devices) {
		t.Fatalf("after deleting the client, %d devices read as unassigned, want %d", len(after), len(devices))
	}

	// 2.3-6: the ledger's refusal rows are the ones Postgres would reject a
	// careless schema for. A failed login has no authenticated user id; a bulk
	// command refused before any target resolved has no entity id; a row about
	// the installation has no client. On a `uuid` column all three are the
	// string Postgres refuses to store, so the empty-actor row is written here
	// and read back: the schema must be able to hold the events the ledger
	// exists to record, not only the tidy successes.
	refusal := &models.AuditLog{
		ID:            models.NewAuditID(),
		ActorUserID:   "",
		ActorUsername: "someone-at-a-keyboard",
		ActorRoles:    "viewer",
		Action:        models.AuditDeviceDelete,
		TargetType:    "route",
		TargetID:      "",
		ClientID:      "",
		Outcome:       models.AuditOutcomeRefused,
		Details:       `{"route":"/api/devices/:id"}`,
	}
	if err := st.Audit.Record(refusal); err != nil {
		t.Fatalf("record a refusal row: %v", err)
	}
	rows, err := st.Audit.List(AuditFilter{Action: models.AuditDeviceDelete, Limit: 10})
	if err != nil {
		t.Fatalf("list the ledger: %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.ID == refusal.ID {
			found = true
			if r.ActorUserID != "" || r.TargetID != "" || r.ClientID != "" {
				t.Errorf("the refusal row came back with ids it never had: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("the refusal row is not in the ledger")
	}

	// The ledger's two composite indexes are the queries the page runs: a
	// manager's client-scoped newest-first read, and "every failed login, this
	// week". Both are selective together, so a single-column index on
	// `created_at` serves neither — and an index is the part a migration
	// forgets without anything noticing until the page is slow.
	for _, probe := range []struct{ table, want string }{
		{"audit_log", "client_id"},
		{"audit_log", "action"},
	} {
		var indexed bool
		if err := st.DB.Raw(
			"SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = ? AND indexdef ILIKE ?)",
			probe.table, "%"+probe.want+"%",
		).Row().Scan(&indexed); err != nil {
			t.Fatalf("index probe on %s: %v", probe.table, err)
		}
		if !indexed {
			t.Errorf("%s has no index covering %s", probe.table, probe.want)
		}
	}
}
