package store

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"gorm.io/gorm"

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
		// 2.3-4c's tokens reference `clients`, so they go first: Postgres
		// refuses to drop a table another table's foreign key points at, and a
		// rollback that quietly failed here would leave a 2.3 table in a 2.2
		// schema and an upgrade test that proves nothing.
		"DROP TABLE IF EXISTS client_enrollment_tokens",
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

	// 2.3-4c ships with 2.3: the credential table must come forward with the
	// rest of the schema, and the unique index on the hash is not decoration —
	// registration walks it, once per enrolled agent.
	if !st.DB.Migrator().HasTable(&models.ClientEnrollmentToken{}) {
		t.Fatal("the upgraded schema has no client_enrollment_tokens table")
	}
	var tokenIndex bool
	if err := st.DB.Raw(
		"SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE tablename = 'client_enrollment_tokens' AND indexdef ILIKE '%token_hash%')",
	).Row().Scan(&tokenIndex); err != nil {
		t.Fatalf("token index probe: %v", err)
	}
	if !tokenIndex {
		t.Error("client_enrollment_tokens has no index on token_hash; enrollment would scan every credential")
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

// seqScanRE matches a plan node that reads `alerts` sequentially: the shape
// failure 2.3-8 exists to catch — every scoped list request reading the whole
// installation's alert history and discarding most of it.
var seqScanRE = regexp.MustCompile(`"Node Type": "Seq Scan"[\s\S]{0,120}?"Relation Name": "alerts"`)

// TestPostgresScopedReadUsesTheIndexes is 2.3-8's EXPLAIN criterion, run on the
// engine production uses. SQLite will take any plan; the statement "scoping is a
// semi-join over the client set, and the planner can walk it" is only a fact on
// Postgres, so the test asks the planner.
func TestPostgresScopedReadUsesTheIndexes(t *testing.T) {
	st := pgStore(t)

	client, err := st.Clients.Create(&models.Client{ID: uuid.NewString(), Name: "Index Co", Slug: "index-co"})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	// A tenant with a real population: a planner with a handful of rows picks a
	// sequential scan for reasons of its own, and the criterion is about the
	// plan at the size the release is built for.
	ids := make([]string, 0, 400)
	for i := 0; i < 400; i++ {
		label := fmt.Sprintf("idx-%03d", i)
		id := uuid.NewString()
		ids = append(ids, id)
		if err := st.Devices.Create(&models.Device{
			ID: id, Name: label, Hostname: label, OS: "linux", Arch: "amd64",
			Status: "online", Tags: []string{}, DeviceKey: "k-" + id, ClientID: &client.ID,
		}); err != nil {
			t.Fatalf("create device: %v", err)
		}
		if err := st.Alerts.Create(&models.Alert{
			ID: uuid.NewString(), DeviceID: id, DeviceName: label, Severity: "warning",
			Message: "signal", Metric: "disk", Value: 91, Threshold: 90,
		}); err != nil {
			t.Fatalf("create alert: %v", err)
		}
	}

	// The indexes migration 10 is named for. Their absence is invisible in a
	// correctness suite and visible as latency in production.
	for _, probe := range []struct{ table, want string }{
		{"devices", "client_id"},
		{"alerts", "device_id"},
		{"sessions", "device_id"},
		{"file_transfers", "device_id"},
		{"software_updates", "device_id"},
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

	// Ask the planner for the scoped read the alert ledger runs. Sequential
	// scans are disabled for this transaction so the answer is "can this plan
	// use the indexes", not "is this table small enough to walk".
	var plan string
	err = st.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
			return err
		}
		if err := tx.Exec("ANALYZE devices, alerts").Error; err != nil {
			return err
		}
		return tx.Raw(
			"EXPLAIN (FORMAT JSON) SELECT * FROM alerts WHERE device_id IN (SELECT id FROM devices WHERE client_id IN (?)) ORDER BY created_at DESC",
			client.ID,
		).Row().Scan(&plan)
	})
	if err != nil {
		t.Fatalf("explain scoped read: %v", err)
	}
	// What the plan must show, and what it must not.
	//
	// The fleet side must be walked by the client index — that is the whole
	// point of scoping by client. The entity side must be an index access: a
	// sequential scan on `alerts` means every scoped list request reads the
	// installation's alert history and throws most of it away, which is the
	// cost this phase is measured against.
	//
	// Which alerts index the planner picks is its business. At 400 units it
	// takes the single-column `device_id` index and sorts the survivors; the
	// composite `(device_id, created_at)` index above is what serves the read
	// when the tenant is large enough that the sort is the expensive part.
	// Asserting the index *name* here would be asserting the planner's mood.
	if !strings.Contains(plan, "idx_devices_client_id") {
		t.Errorf("the scoped alert read does not walk the client index:\n%s", plan)
	}
	if !strings.Contains(plan, "\"Index Name\": \"idx_alerts") {
		t.Errorf("the scoped alert read does not use an alerts index:\n%s", plan)
	}
	if seqScanRE.MatchString(plan) {
		t.Errorf("the scoped alert read scans the whole alerts table:\n%s", plan)
	}
}
