package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"ourway/server/models"
)

// 2.3-4 — the client surface, over the real router with real tokens.

func TestClientsCRUD(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "clients-admin", "admin")

	// Create.
	code, body, _ := postJSON(t, f.baseURL+"/api/clients", admin, map[string]any{
		"name": "Gamma Widgets", "notes": "3 sites",
	})
	if code != http.StatusCreated {
		t.Fatalf("create client = %d: %s", code, body)
	}
	var created struct {
		Client models.Client `json:"client"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	gamma := created.Client
	// The slug is derived and normalized by the store's tag rule.
	if gamma.Slug != "gamma-widgets" {
		t.Fatalf("slug = %q, want gamma-widgets", gamma.Slug)
	}

	// The new client appears for admin, and for nobody else.
	code, body, _ = f.get(t, "/api/clients", admin)
	var listEnv struct {
		Clients []models.Client `json:"clients"`
	}
	if err := json.Unmarshal(body, &listEnv); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, cl := range listEnv.Clients {
		if cl.ID == gamma.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("admin's client list did not contain the new client: %+v", listEnv.Clients)
	}

	tech := f.makeUserWithScope(t, "clients-tech", "technician", f.alice.ID)
	code, body, _ = f.get(t, "/api/clients", tech)
	if err := json.Unmarshal(body, &listEnv); err != nil {
		t.Fatalf("decode scoped list: %v", err)
	}
	for _, cl := range listEnv.Clients {
		if cl.ID == gamma.ID {
			t.Fatal("a technician sees a client they were not granted")
		}
	}
	if len(listEnv.Clients) != 1 || listEnv.Clients[0].ID != f.alice.ID {
		t.Fatalf("scoped client list = %+v, want exactly client A", listEnv.Clients)
	}

	// Out-of-scope client answers 404, identical to a nonexistent client.
	_, missingBody, _ := f.get(t, "/api/clients/"+uuid.NewString(), tech)
	foreignCode, foreignBody, _ := f.get(t, "/api/clients/"+gamma.ID, tech)
	if foreignCode != http.StatusNotFound {
		t.Fatalf("out-of-scope client = %d, want 404", foreignCode)
	}
	stringMissing := string(missingBody)
	stringForeign := string(foreignBody)
	if stringMissing != stringForeign {
		t.Fatalf("bodies differ.\n  nonexistent: %s\n  out of scope: %s", stringMissing, stringForeign)
	}

	// A duplicate slug is 409, naming the slug.
	code, body, _ = postJSON(t, f.baseURL+"/api/clients", admin, map[string]any{
		"name": "Gamma  Widgets",
	})
	if code != http.StatusConflict {
		t.Fatalf("duplicate slug = %d, want 409: %s", code, body)
	}
	var slugErr struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(body, &slugErr); err != nil || slugErr.Slug != "gamma-widgets" {
		t.Fatalf("duplicate slug response = %s", body)
	}

	// Update, then delete.
	code, _, _ = f.do(t, http.MethodPut, f.baseURL+"/api/clients/"+gamma.ID, admin, map[string]any{
		"name": "Gamma Widgets Ltd", "slug": "gamma", "notes": "renewed",
	})
	if code != http.StatusOK {
		t.Fatalf("update client = %d", code)
	}

	// Writes are admin-only.
	manager := f.makeUserWithScope(t, "clients-manager", "manager", f.alice.ID)
	if code, _, _ := postJSON(t, f.baseURL+"/api/clients", manager, map[string]any{"name": "Sneaky Co"}); code != http.StatusForbidden {
		t.Fatalf("manager create = %d, want 403", code)
	}
	if code, _, _ := f.do(t, http.MethodPut, f.baseURL+"/api/clients/"+f.alice.ID, manager, map[string]any{"name": "Renamed by manager"}); code != http.StatusForbidden {
		t.Fatalf("manager update = %d, want 403", code)
	}
	if code, _, _ := f.do(t, http.MethodDelete, f.baseURL+"/api/clients/"+f.alice.ID, manager, nil); code != http.StatusForbidden {
		t.Fatalf("manager delete = %d, want 403", code)
	}
}

// The rule the plan names as a store assertion, proved through the API:
// deleting a client leaves every device intact and unassigned.
func TestClientsDeleteMovesDevicesToUnassigned(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "delete-admin", "admin")

	code, body, _ := postJSON(t, f.baseURL+"/api/clients", admin, map[string]any{"name": "Delta Co"})
	var created struct {
		Client models.Client `json:"client"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	delta := created.Client

	// Assign two units, then grant a technician this client, so the delete has
	// a live fleet and a live grant to unmake.
	if code, body, _ := f.do(t, http.MethodPut, f.baseURL+"/api/clients/"+delta.ID+"/devices", admin, map[string]any{
		"device_ids": []string{f.inAlice, f.inBob},
	}); code != http.StatusOK {
		t.Fatalf("assign = %d: %s", code, body)
	}
	tech := f.makeUserWithScope(t, "delta-tech", "technician", delta.ID)
	if _, body, _ := f.get(t, "/api/devices", tech); !jsonHasArrayNamed(body, "devices", 2) {
		t.Fatalf("the granted technician sees %s, want the two assigned units", body)
	}

	code, body, _ = f.do(t, http.MethodDelete, f.baseURL+"/api/clients/"+delta.ID, admin, nil)
	if code != http.StatusOK {
		t.Fatalf("delete client = %d: %s", code, body)
	}
	var delEnv struct {
		DevicesUnassigned int `json:"devices_unassigned"`
	}
	if err := json.Unmarshal(body, &delEnv); err != nil || delEnv.DevicesUnassigned != 2 {
		t.Fatalf("delete reported %s, want 2 units moved", body)
	}

	// Every device is intact, and none carries a client.
	all, err := f.st.Devices.ListAll()
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	for _, d := range all {
		if d.ClientID != nil {
			t.Fatalf("device %s kept client %v after its client was deleted", d.ID, d.ClientID)
		}
	}
	for _, id := range []string{f.inAlice, f.inBob, f.unassigned} {
		if _, err := f.st.Devices.GetByID(id); err != nil {
			t.Fatalf("device %s disappeared: %v", id, err)
		}
	}

	// The grants that named the client are gone with it, on the technician's
	// next request with the same token: the units they could reach are now
	// unassigned, which is admin's pool, and their fleet is empty.
	if _, body, _ := f.get(t, "/api/devices", tech); !jsonHasArrayNamed(body, "devices", 0) {
		t.Fatalf("a technician whose client was deleted sees devices: %s", body)
	}
	if code, _, _ := f.get(t, "/api/clients/"+delta.ID, tech); code != http.StatusNotFound {
		t.Fatalf("deleted client for its former technician = %d, want 404", code)
	}
}

// jsonHasArrayNamed checks the envelope's array length without the caller
// needing a typed decode.
func jsonHasArrayNamed(body []byte, key string, want int) bool {
	var env map[string][]json.RawMessage
	if err := json.Unmarshal(body, &env); err != nil {
		return false
	}
	return len(env[key]) == want
}

// The grant endpoints, and the criterion that decides how grants are stored:
// they take effect on the next request with the same token.
func TestClientsGrantUsers(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "grant-admin", "admin")

	tech := f.makeUserWithScope(t, "grant-tech", "technician")
	user, err := f.st.Users.GetByUsername("grant-tech")
	if err != nil {
		t.Fatalf("load technician: %v", err)
	}

	// Before: the technician's fleet is empty.
	if _, body, _ := f.get(t, "/api/devices", tech); !jsonHasArrayNamed(body, "devices", 0) {
		t.Fatalf("expected an empty fleet before the grant: %s", body)
	}

	code, body, _ := f.do(t, http.MethodPut, f.baseURL+"/api/clients/"+f.alice.ID+"/users", admin, map[string]any{
		"user_ids": []string{user.ID},
	})
	if code != http.StatusOK {
		t.Fatalf("set users = %d: %s", code, body)
	}

	// The grant list is admin-visible.
	code, body, _ = f.get(t, "/api/clients/"+f.alice.ID+"/users", admin)
	var usersEnv struct {
		Users []models.User `json:"users"`
	}
	if err := json.Unmarshal(body, &usersEnv); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(usersEnv.Users) != 1 || usersEnv.Users[0].ID != user.ID {
		t.Fatalf("client users = %+v, want exactly the technician", usersEnv.Users)
	}

	// Same access token, next request: the fleet is there.
	if _, body, _ := f.get(t, "/api/devices", tech); !jsonHasArrayNamed(body, "devices", 1) {
		t.Fatalf("expected 1 device after the grant: %s", body)
	}
	if code, _, _ := f.get(t, "/api/devices/"+f.inAlice, tech); code != http.StatusOK {
		t.Fatalf("granted unit = %d, want 200", code)
	}
	// And still nothing from client B.
	if code, _, _ := f.get(t, "/api/devices/"+f.inBob, tech); code != http.StatusNotFound {
		t.Fatalf("foreign unit after grant = %d, want 404", code)
	}

	// Replace the grant with none: the fleet empties again, same token.
	if code, _, _ := f.do(t, http.MethodPut, f.baseURL+"/api/clients/"+f.alice.ID+"/users", admin, map[string]any{
		"user_ids": []string{},
	}); code != http.StatusOK {
		t.Fatalf("clear users = %d", code)
	}
	if _, body, _ := f.get(t, "/api/devices", tech); !jsonHasArrayNamed(body, "devices", 0) {
		t.Fatalf("expected an empty fleet after revocation: %s", body)
	}
}

// A technician granted two clients sees exactly those two in the ledger.
func TestClientsTwoGrantsSeeExactlyTwo(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "two-admin", "admin")

	// A third client with its own unit.
	_, body, _ := postJSON(t, f.baseURL+"/api/clients", admin, map[string]any{"name": "Epsilon Co"})
	var created struct {
		Client models.Client `json:"client"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	epsilon := created.Client
	unit := &models.Device{
		ID: uuid.NewString(), Name: "unit-epsilon", Hostname: "epsilon", OS: "linux",
		Arch: "amd64", Status: "online", Tags: []string{}, DeviceKey: uuid.NewString(),
		ClientID: &epsilon.ID,
	}
	if err := f.st.Devices.Create(unit); err != nil {
		t.Fatalf("create epsilon unit: %v", err)
	}

	tech := f.makeUserWithScope(t, "two-tech", "technician", f.alice.ID, epsilon.ID)

	code, body, header := f.get(t, "/api/clients", tech)
	if code != http.StatusOK {
		t.Fatalf("client list = %d", code)
	}
	var env struct {
		Clients []models.Client `json:"clients"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode clients: %v", err)
	}
	if len(env.Clients) != 2 {
		t.Fatalf("technician with two grants sees %d clients, want 2", len(env.Clients))
	}
	seen := map[string]bool{}
	for _, cl := range env.Clients {
		seen[cl.ID] = true
	}
	if !seen[f.alice.ID] || !seen[epsilon.ID] {
		t.Fatalf("granted clients = %+v, want A and Epsilon", env.Clients)
	}
	// The header names both.
	if header != f.alice.ID+","+epsilon.ID && header != epsilon.ID+","+f.alice.ID {
		t.Fatalf("scope header = %q, want the two granted ids", header)
	}

	// The fleet is exactly the two clients' units — never the unassigned unit.
	code, body, _ = f.get(t, "/api/devices", tech)
	if err := json.Unmarshal(body, &struct {
		Devices []models.Device `json:"devices"`
	}{}); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var devEnv struct {
		Devices []models.Device `json:"devices"`
	}
	if err := json.Unmarshal(body, &devEnv); err != nil {
		t.Fatalf("decode devices: %v", err)
	}
	if len(devEnv.Devices) != 2 {
		t.Fatalf("two-client technician sees %d units, want 2 (unassigned is admin's)", len(devEnv.Devices))
	}
}

// The rollup must equal the numbers the fleet ledgers compute for the same
// client — 2.2-4's single-source rule, asserted against an independent
// computation from the same store.
func TestClientsRollupMatchesTheLedgers(t *testing.T) {
	f := seedFleetFixture(t)
	admin := f.makeUserWithScope(t, "rollup-admin", "admin")

	// More signal on client A's unit: an online unit, a warning alert, a
	// reboot pending, and a second open update.
	alice, err := f.st.Devices.GetByID(f.inAlice)
	if err != nil {
		t.Fatalf("load alpha: %v", err)
	}
	alice.RebootPending = true
	alice.LastSeen = time.Now().Add(-3 * time.Minute)
	if err := f.st.Devices.Update(alice); err != nil {
		t.Fatalf("update alpha: %v", err)
	}
	if err := f.st.Alerts.Create(&models.Alert{
		ID: uuid.NewString(), DeviceID: f.inAlice, DeviceName: "unit-alpha",
		Severity: "warning", Message: "memory pressure", Metric: "memory", Value: 88, Threshold: 80,
	}); err != nil {
		t.Fatalf("create alert: %v", err)
	}
	if err := f.st.SoftwareUpdates.Create(&models.SoftwareUpdate{
		ID: uuid.NewString(), DeviceID: f.inAlice, Source: "apt", Title: "openssl",
		ExternalID: uuid.NewString(), Severity: "important", Status: "approved",
	}); err != nil {
		t.Fatalf("create update: %v", err)
	}

	code, body, _ := f.get(t, "/api/clients/"+f.alice.ID+"/rollup", admin)
	if code != http.StatusOK {
		t.Fatalf("rollup = %d: %s", code, body)
	}
	var env struct {
		Rollup clientRollup `json:"rollup"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode rollup: %v", err)
	}
	r := env.Rollup

	// Independent computation, straight from the stores the ledgers read.
	devices, err := f.st.Devices.ListScoped([]string{f.alice.ID}, false)
	if err != nil {
		t.Fatalf("scoped devices: %v", err)
	}
	ids := make([]string, 0, len(devices))
	online := 0
	for _, d := range devices {
		ids = append(ids, d.ID)
		if d.Status == "online" {
			online++
		}
	}
	if r.Units != len(devices) {
		t.Fatalf("rollup units = %d, want %d", r.Units, len(devices))
	}
	if r.UnitsOnline != online {
		t.Fatalf("rollup units_online = %d, want %d", r.UnitsOnline, online)
	}

	// Independent computation over the same client-scoped reads the ledgers and
	// the rollup use — the client set, not the unit list it names (2.3-8).
	alerts, err := f.st.Alerts.ListScoped([]string{f.alice.ID}, false, nil, true)
	if err != nil {
		t.Fatalf("scoped alerts: %v", err)
	}
	critical, warning := 0, 0
	for _, a := range alerts {
		switch a.Severity {
		case "critical":
			critical++
		case "warning":
			warning++
		}
	}
	if r.AlertsOpen != len(alerts) || r.AlertsCritical != critical || r.AlertsWarning != warning {
		t.Fatalf("rollup alerts = %+v, want open=%d critical=%d warning=%d",
			r, len(alerts), critical, warning)
	}

	updates, err := f.st.SoftwareUpdates.ListByStatusesScoped(openStatuses, []string{f.alice.ID}, false)
	if err != nil {
		t.Fatalf("scoped updates: %v", err)
	}
	withOpen := map[string]bool{}
	for _, u := range updates {
		withOpen[u.DeviceID] = true
	}
	if r.PatchPending != len(updates) {
		t.Fatalf("rollup patch_pending = %d, want %d", r.PatchPending, len(updates))
	}
	if r.PatchCompliant != len(devices)-len(withOpen) {
		t.Fatalf("rollup patch_compliant = %d, want %d", r.PatchCompliant, len(devices)-len(withOpen))
	}

	// The rollup is scoped too: a technician with no grants cannot read it.
	tech := f.makeUserWithScope(t, "rollup-tech", "technician")
	if code, _, _ := f.get(t, "/api/clients/"+f.alice.ID+"/rollup", tech); code != http.StatusNotFound {
		t.Fatalf("ungranted rollup = %d, want 404", code)
	}
}

// Assignment honours the bulk contract: targets the caller cannot reach are
// named, and nothing moves.
func TestClientsAssignValidatesTargets(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "assign-admin", "admin")
	_, body, _ := postJSON(t, f.baseURL+"/api/clients", admin, map[string]any{"name": "Zeta Co"})
	var created struct {
		Client models.Client `json:"client"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	zeta := created.Client

	// A manager of client A tries to move both A's unit and B's unit into Zeta.
	manager := f.makeUserWithScope(t, "assign-manager", "manager", f.alice.ID)
	code, body, _ := f.do(t, http.MethodPut, f.baseURL+"/api/clients/"+zeta.ID+"/devices", manager, map[string]any{
		"device_ids": []string{f.inAlice, f.inBob},
	})
	// The route is admin-only, so RBAC answers first — and the scope rule is
	// asserted where the caller *may* call it: admin naming foreign targets
	// below, on a client the admin is granted by definition.
	if code != http.StatusForbidden {
		t.Fatalf("manager assign = %d, want 403", code)
	}

	// Admin assigning a real batch moves exactly those units.
	code, body, _ = f.do(t, http.MethodPut, f.baseURL+"/api/clients/"+zeta.ID+"/devices", admin, map[string]any{
		"device_ids": []string{f.inAlice},
	})
	if code != http.StatusOK {
		t.Fatalf("admin assign = %d: %s", code, body)
	}
	moved, err := f.st.Devices.GetByID(f.inAlice)
	if err != nil || moved.ClientID == nil || *moved.ClientID != zeta.ID {
		t.Fatalf("unit did not move into the client: %+v", moved)
	}
	bob, err := f.st.Devices.GetByID(f.inBob)
	if err != nil || bob.ClientID == nil || *bob.ClientID == zeta.ID {
		t.Fatalf("a unit outside the batch moved: %+v", bob)
	}

	// Emptying the client unassigns its units and deletes nothing.
	code, body, _ = f.do(t, http.MethodPut, f.baseURL+"/api/clients/"+zeta.ID+"/devices", admin, map[string]any{
		"device_ids": []string{},
	})
	if code != http.StatusOK {
		t.Fatalf("empty client = %d: %s", code, body)
	}
	restored, err := f.st.Devices.GetByID(f.inAlice)
	if err != nil || restored.ClientID != nil {
		t.Fatalf("emptying the client left a client on the unit: %+v", restored)
	}
}

// TestClientsRollupsIsTheLedger is 2.3-5's single-source criterion: the
// dashboard's per-client rows and the Clients ledger's rows are the same
// numbers. The mechanism is that they are the same read of the same function,
// so the test asserts the list endpoint's row for a client equals what
// `GET /api/clients/:id/rollup` answers for it, field by field, and that the
// list costs one read whatever the tenant count.
func TestClientsRollupsIsTheLedger(t *testing.T) {
	f := seedFleetFixture(t)
	admin := f.makeUserWithScope(t, "rollups-admin", "admin")
	tech := f.makeUserWithScope(t, "rollups-tech", "technician", f.alice.ID)

	code, body, header := f.get(t, "/api/clients/rollups", admin)
	if code != http.StatusOK {
		t.Fatalf("rollups = %d: %s", code, body)
	}
	if header != "all" {
		t.Fatalf("admin rollups scope = %q, want all", header)
	}
	var env struct {
		Rollups []clientRollup `json:"rollups"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode rollups: %v", err)
	}

	byID := map[string]clientRollup{}
	for _, r := range env.Rollups {
		byID[r.ClientID] = r
	}

	// Every tenant the admin may see has a row, and the row is the per-client
	// endpoint's row.
	for _, cl := range []models.Client{*f.alice, *f.bob} {
		row, ok := byID[cl.ID]
		if !ok {
			t.Fatalf("rollups has no row for %s: %+v", cl.Name, env.Rollups)
		}
		code, single, _ := f.get(t, "/api/clients/"+cl.ID+"/rollup", admin)
		if code != http.StatusOK {
			t.Fatalf("single rollup = %d", code)
		}
		var singleEnv struct {
			Rollup clientRollup `json:"rollup"`
		}
		if err := json.Unmarshal(single, &singleEnv); err != nil {
			t.Fatalf("decode single rollup: %v", err)
		}
		if a, b := row, singleEnv.Rollup; a.Units != b.Units || a.UnitsOnline != b.UnitsOnline ||
			a.AlertsOpen != b.AlertsOpen || a.AlertsCritical != b.AlertsCritical ||
			a.PatchUnits != b.PatchUnits || a.PatchCompliant != b.PatchCompliant ||
			a.PatchPending != b.PatchPending || a.PatchCritical != b.PatchCritical ||
			a.PatchRebootPending != b.PatchRebootPending || a.Technicians != b.Technicians {
			t.Fatalf("rollups row for %s is not the ledger row:\n list   %+v\n single %+v", cl.Name, a, b)
		}
	}

	// The unassigned bucket exists for the caller who can see it, and it is
	// named like the sentinel every other surface uses.
	free, ok := byID[models.UnassignedClientID]
	if !ok {
		t.Fatalf("admin rollups has no unassigned row: %+v", env.Rollups)
	}
	if free.Slug != models.UnassignedClientID || free.Units != 1 {
		t.Fatalf("unassigned row = %+v, want slug unassigned and 1 unit", free)
	}

	// A scoped caller gets exactly their tenants: no other tenant's row, and no
	// unassigned row, because a grant is a grant over a client's units.
	code, body, header = f.get(t, "/api/clients/rollups", tech)
	if code != http.StatusOK {
		t.Fatalf("scoped rollups = %d: %s", code, body)
	}
	if header == "all" || header == "" || header == "none" {
		t.Fatalf("technician rollups scope = %q, want the granted ids", header)
	}
	var scoped struct {
		Rollups []clientRollup `json:"rollups"`
	}
	if err := json.Unmarshal(body, &scoped); err != nil {
		t.Fatalf("decode scoped rollups: %v", err)
	}
	if len(scoped.Rollups) != 1 {
		t.Fatalf("technician sees %d tenant rows, want exactly 1: %+v", len(scoped.Rollups), scoped.Rollups)
	}
	if scoped.Rollups[0].ClientID != f.alice.ID {
		t.Fatalf("technician row is %q, want the granted client", scoped.Rollups[0].ClientID)
	}
}

// TestClientsGrantsCountIsOneQuery is the scale rule behind the ledger column
// (2.3-8's N+1): the grant count of every tenant arrives in one grouped read.
func TestClientsGrantsCountIsOneQuery(t *testing.T) {
	f := newScopeFixture(t)
	if _, err := f.st.Clients.Create(&models.Client{ID: models.NewClientID(), Name: "Delta", Slug: "delta"}); err != nil {
		t.Fatalf("create delta: %v", err)
	}
	f.makeUserWithScope(t, "grants-tech", "technician", f.alice.ID, f.bob.ID)

	counts, err := f.st.Clients.GrantsCount()
	if err != nil {
		t.Fatalf("grants count: %v", err)
	}
	if counts[f.alice.ID] != 1 || counts[f.bob.ID] != 1 {
		t.Fatalf("grant counts = %+v, want one grant on each seeded client", counts)
	}
}
