package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"ourway/server/models"
)

// 2.3-2 — every route that reaches a unit, and every fleet list, respects the
// caller's clients.
//
// The matrix the acceptance names is run per *kind* of route, and the comparison
// is per *byte*: for each kind, the answer for a unit in another client must
// equal the answer for a unit that does not exist. Writing one route's scoping
// and trusting the others to be similar is exactly how a release like this
// leaks, so the test enumerates.

// seedFleetFixture extends 2.3-1's two-client fleet with the entities that
// reference a unit: an alert and a detected update on each side, a deployment
// over both, and a file transfer that pulled from each.
func seedFleetFixture(t *testing.T) *scopeFixture {
	f := newScopeFixture(t)

	mkAlert := func(deviceID, name string) string {
		a := &models.Alert{
			ID: uuid.NewString(), DeviceID: deviceID, DeviceName: name,
			Severity: "critical", Message: "disk full on " + name, Metric: "disk",
			Value: 97, Threshold: 90,
		}
		if err := f.st.Alerts.Create(a); err != nil {
			t.Fatalf("create alert: %v", err)
		}
		return a.ID
	}
	mkUpdate := func(deviceID string) string {
		u := &models.SoftwareUpdate{
			ID: uuid.NewString(), DeviceID: deviceID, Source: "windows",
			Title: "Cumulative update", ExternalID: uuid.NewString(),
			Severity: "critical", Status: "detected",
		}
		if err := f.st.SoftwareUpdates.Create(u); err != nil {
			t.Fatalf("create update: %v", err)
		}
		return u.ID
	}

	alertAlice := mkAlert(f.inAlice, "unit-alpha")
	alertBob := mkAlert(f.inBob, "unit-beta")
	updateAlice := mkUpdate(f.inAlice)
	updateBob := mkUpdate(f.inBob)

	deployment := &models.PatchDeployment{
		ID: uuid.NewString(), Status: "completed",
		DeviceIDs: []string{f.inAlice, f.inBob},
	}
	if err := f.st.PatchDeployments.Create(deployment); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	transfer := &models.FileTransfer{
		ID: uuid.NewString(), DeviceID: f.inBob, Filename: "secrets.txt",
		Direction: "pull", Status: "completed",
	}
	if err := f.st.FileTransfers.Create(transfer); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	f.extra = map[string]string{
		"alert_alice": alertAlice, "alert_bob": alertBob,
		"update_alice": updateAlice, "update_bob": updateBob,
		"deployment": deployment.ID, "transfer_bob": transfer.ID,
	}
	return f
}

func postJSON(t *testing.T, url, token string, body any) (int, []byte, string) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out, res.Header.Get("X-OurWay-Client-Scope")
}

func putJSON(t *testing.T, url, token string, body any) (int, []byte, string) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put %s: %v", url, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out, res.Header.Get("X-OurWay-Client-Scope")
}

// The matrix: for each by-device route kind, the out-of-scope answer must equal
// the nonexistent answer — status and body.
func TestScopeEveryDeviceRouteKindAnswersTheSameNotFound(t *testing.T) {
	f := seedFleetFixture(t)
	ghost := uuid.NewString()

	// A fresh caller per case. Two reasons, and the second is the interesting
	// one: the per-user rate limiter answers a burst of POSTs with 429, so a
	// matrix that reuses one token measures its own request count instead of
	// scoping; and every case needs its own scope so no case inherits another's
	// grants.
	cases := []struct {
		name   string
		role   string
		method string
		path   func(id string) string
		body   any
	}{
		{"device", "technician", http.MethodGet, func(id string) string { return "/api/devices/" + id }, nil},
		{"latest metrics", "technician", http.MethodGet, func(id string) string { return "/api/devices/" + id + "/metrics" }, nil},
		{"metrics history", "technician", http.MethodGet, func(id string) string { return "/api/devices/" + id + "/metrics/history" }, nil},
		{"set tags", "technician", http.MethodPut, func(id string) string { return "/api/devices/" + id + "/tags" }, map[string]any{"tags": []string{"x"}}},
		{"start session", "technician", http.MethodPost, func(id string) string { return "/api/devices/" + id + "/sessions" }, map[string]any{}},
		{"list updates", "technician", http.MethodGet, func(id string) string { return "/api/devices/" + id + "/updates" }, nil},
		{"scan device", "technician", http.MethodPost, func(id string) string { return "/api/devices/" + id + "/updates/scan" }, map[string]any{}},
		{"reboot device", "technician", http.MethodPost, func(id string) string { return "/api/devices/" + id + "/reboot" }, map[string]any{}},
		{"start stream", "technician", http.MethodPost, func(id string) string { return "/api/devices/" + id + "/stream" }, map[string]any{"interval": 2}},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := f.makeUserWithScope(t, fmt.Sprintf("matrix-%d", i), tc.role, f.alice.ID)
			_, missingBody, _ := f.do(t, tc.method, f.baseURL+tc.path(ghost), token, tc.body)
			foreignCode, foreignBody, header := f.do(t, tc.method, f.baseURL+tc.path(f.inBob), token, tc.body)

			if foreignCode != http.StatusNotFound {
				t.Fatalf("%s: out-of-scope answered %d (%s), want 404", tc.name, foreignCode, foreignBody)
			}
			if !bytes.Equal(missingBody, foreignBody) {
				t.Fatalf("%s: bodies differ.\n  nonexistent: %s\n  out of scope: %s", tc.name, missingBody, foreignBody)
			}
			if header != f.alice.ID {
				t.Fatalf("%s: scope header = %q, want %q", tc.name, header, f.alice.ID)
			}
		})
	}
}

// Fleet lists carry no foreign rows.
func TestScopeFleetListsCarryNoForeignRows(t *testing.T) {
	f := seedFleetFixture(t)
	token := f.makeUserWithScope(t, "fleet-tech", "manager", f.alice.ID)

	// Alerts.
	code, body, _ := f.get(t, "/api/alerts", token)
	if code != http.StatusOK {
		t.Fatalf("alerts = %d, want 200", code)
	}
	var alertEnv struct {
		Alerts []models.Alert `json:"alerts"`
	}
	if err := json.Unmarshal(body, &alertEnv); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	for _, a := range alertEnv.Alerts {
		if a.DeviceID == f.inBob {
			t.Fatal("the alert list carried an alert about a beta unit")
		}
	}
	if len(alertEnv.Alerts) != 1 || alertEnv.Alerts[0].ID != f.extra["alert_alice"] {
		t.Fatalf("alert list = %+v, want exactly the alpha unit's alert", alertEnv.Alerts)
	}

	// Fleet update groups.
	code, body, _ = f.get(t, "/api/patch/updates", token)
	if code != http.StatusOK {
		t.Fatalf("fleet updates = %d, want 200", code)
	}
	var fleetEnv struct {
		Updates []struct {
			UpdateIDs []string `json:"update_ids"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(body, &fleetEnv); err != nil {
		t.Fatalf("decode fleet updates: %v", err)
	}
	all := []string{}
	for _, g := range fleetEnv.Updates {
		all = append(all, g.UpdateIDs...)
	}
	if len(all) != 1 || all[0] != f.extra["update_alice"] {
		t.Fatalf("fleet update ids = %v, want exactly the alpha unit's update", all)
	}

	// Patch overview counts.
	code, body, _ = f.get(t, "/api/patch/overview", token)
	if code != http.StatusOK {
		t.Fatalf("overview = %d, want 200", code)
	}
	var overview struct {
		Devices []struct {
			DeviceID string `json:"device_id"`
		} `json:"devices"`
		Totals struct {
			Devices int `json:"devices"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(body, &overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if len(overview.Devices) != 1 || overview.Devices[0].DeviceID != f.inAlice {
		t.Fatalf("overview rows = %+v, want exactly the alpha unit", overview.Devices)
	}

	// Deployments that touch a foreign unit are not visible at all.
	code, body, _ = f.get(t, "/api/patch/deployments", token)
	if code != http.StatusOK {
		t.Fatalf("deployments = %d, want 200", code)
	}
	var depEnv struct {
		Deployments []models.PatchDeployment `json:"deployments"`
	}
	if err := json.Unmarshal(body, &depEnv); err != nil {
		t.Fatalf("decode deployments: %v", err)
	}
	if len(depEnv.Deployments) != 0 {
		t.Fatalf("a deployment spanning both clients was listed for a single-client manager: %+v", depEnv.Deployments)
	}

	// File transfers.
	code, body, _ = f.get(t, "/api/files/transfers", token)
	if code != http.StatusOK {
		t.Fatalf("transfers = %d, want 200", code)
	}
	var transferEnv struct {
		Transfers []models.FileTransfer `json:"transfers"`
	}
	if err := json.Unmarshal(body, &transferEnv); err != nil {
		t.Fatalf("decode transfers: %v", err)
	}
	for _, tr := range transferEnv.Transfers {
		if tr.DeviceID == f.inBob {
			t.Fatal("the transfer list carried a pull from a beta unit")
		}
	}
}

// The entity routes (alert by id, update by id, deployment by id, transfer by id)
// answer their own not-found for a foreign unit.
func TestScopeEntityRoutesAnswerTheirOwnNotFound(t *testing.T) {
	f := seedFleetFixture(t)

	// The role column is the route's own gate: scoping is tested against a
	// caller who is *allowed* the action, so a 403 from RBAC can never be
	// mistaken for the 404 scoping is supposed to answer.
	cases := []struct {
		name   string
		role   string
		method string
		path   string
		body   any
	}{
		{"resolve alert", "technician", http.MethodPost, "/api/alerts/" + f.extra["alert_bob"] + "/resolve", map[string]any{}},
		{"acknowledge alert", "technician", http.MethodPost, "/api/alerts/" + f.extra["alert_bob"] + "/acknowledge", map[string]any{}},
		{"assign alert", "technician", http.MethodPost, "/api/alerts/" + f.extra["alert_bob"] + "/assign", map[string]any{"user_id": "someone"}},
		{"approve update", "manager", http.MethodPost, "/api/updates/" + f.extra["update_bob"] + "/approve", map[string]any{}},
		{"deployment results", "technician", http.MethodGet, "/api/patch/deployments/" + f.extra["deployment"] + "/results", nil},
		{"deployment rollback", "manager", http.MethodPost, "/api/patch/deployments/" + f.extra["deployment"] + "/rollback", map[string]any{}},
		{"get transfer", "technician", http.MethodGet, "/api/files/transfers/" + f.extra["transfer_bob"], nil},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := f.makeUserWithScope(t, fmt.Sprintf("entity-%d", i), tc.role, f.alice.ID)
			code, body, _ := f.do(t, tc.method, f.baseURL+tc.path, token, tc.body)
			if code != http.StatusNotFound {
				t.Fatalf("%s: %d (%s), want 404", tc.name, code, body)
			}
		})
	}

	// The in-scope alert resolves for a caller who holds the action, so the 404
	// above is scoping and not a broken route.
	tech := f.makeUserWithScope(t, "entity-can-do", "technician", f.alice.ID)
	if code, body, _ := f.do(t, http.MethodPost, f.baseURL+"/api/alerts/"+f.extra["alert_alice"]+"/resolve", tech, map[string]any{}); code != http.StatusOK {
		t.Fatalf("in-scope alert resolve = %d: %s", code, body)
	}
}

// Bulk targets fail naming the ids, with nothing written.
func TestScopeBulkTargetsFailNamingTheIds(t *testing.T) {
	f := seedFleetFixture(t)
	// One caller per bulk call, for the same two reasons as the matrix above.
	token := f.makeUserWithScope(t, "bulk-tags", "manager", f.alice.ID)

	// Bulk tags: one alpha unit, one beta unit. The beta id must be named and
	// the alpha unit must be left alone.
	code, body, _ := postJSON(t, f.baseURL+"/api/devices/tags/bulk", token, map[string]any{
		"device_ids": []string{f.inAlice, f.inBob},
		"add":        []string{"patched"},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("bulk tags = %d, want 400: %s", code, body)
	}
	var tagErr struct {
		Error     string   `json:"error"`
		DeviceIDs []string `json:"device_ids"`
	}
	if err := json.Unmarshal(body, &tagErr); err != nil {
		t.Fatalf("decode bulk tags error: %v", err)
	}
	if len(tagErr.DeviceIDs) != 1 || tagErr.DeviceIDs[0] != f.inBob {
		t.Fatalf("bulk tags named %v, want exactly [%s]", tagErr.DeviceIDs, f.inBob)
	}
	alpha, err := f.st.Devices.GetByID(f.inAlice)
	if err != nil {
		t.Fatalf("reload alpha: %v", err)
	}
	if len(alpha.Tags) != 0 {
		t.Fatalf("the refused bulk call wrote tags: %v", alpha.Tags)
	}

	// Bulk approve: same rule for update ids.
	token = f.makeUserWithScope(t, "bulk-approve", "manager", f.alice.ID)
	code, body, _ = postJSON(t, f.baseURL+"/api/patch/updates/approve", token, map[string]any{
		"update_ids": []string{f.extra["update_alice"], f.extra["update_bob"]},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("bulk approve = %d, want 400: %s", code, body)
	}
	var updateErr struct {
		UpdateIDs []string `json:"update_ids"`
	}
	if err := json.Unmarshal(body, &updateErr); err != nil {
		t.Fatalf("decode bulk approve error: %v", err)
	}
	if len(updateErr.UpdateIDs) != 1 || updateErr.UpdateIDs[0] != f.extra["update_bob"] {
		t.Fatalf("bulk approve named %v, want exactly the beta unit's update", updateErr.UpdateIDs)
	}
	aliceUpdate, err := f.st.SoftwareUpdates.GetByID(f.extra["update_alice"])
	if err != nil {
		t.Fatalf("reload alpha update: %v", err)
	}
	if aliceUpdate.Status != "detected" {
		t.Fatalf("the refused bulk approve changed status to %q", aliceUpdate.Status)
	}

	// Deploy: a foreign target is refused naming it.
	token = f.makeUserWithScope(t, "bulk-deploy", "manager", f.alice.ID)
	code, _, _ = postJSON(t, f.baseURL+"/api/patch/deploy", token, map[string]any{
		"device_ids": []string{f.inBob},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("deploy to a foreign unit = %d, want 400", code)
	}

	// File push to a foreign unit is refused naming it.
	token = f.makeUserWithScope(t, "bulk-push", "manager", f.alice.ID)
	code, _, _ = postJSON(t, f.baseURL+"/api/files/push", token, map[string]any{
		"transfer_id": f.extra["transfer_bob"],
		"device_ids":  []string{f.inBob},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("file push to a foreign unit = %d, want 400", code)
	}

	// Patch scan naming a foreign unit is refused naming it.
	token = f.makeUserWithScope(t, "bulk-scan", "technician", f.alice.ID)
	code, _, _ = postJSON(t, f.baseURL+"/api/patch/scan", token, map[string]any{
		"device_ids": []string{f.inBob},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("scan naming a foreign unit = %d, want 400", code)
	}
}

// "Scan everything" for a scoped caller means their units, never the
// installation: the empty-target form of a command is the easy one to forget.
func TestScopeEmptyBulkTargetMeansTheCallersOwnFleet(t *testing.T) {
	f := seedFleetFixture(t)
	token := f.makeUserWithScope(t, "scan-tech", "technician", f.alice.ID)

	code, body, _ := postJSON(t, f.baseURL+"/api/patch/scan", token, map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("scan all = %d, want 200: %s", code, body)
	}
	var res struct {
		ScansSent int `json:"scans_sent"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode scan result: %v", err)
	}
	if res.ScansSent > 1 {
		t.Fatalf("an empty-target scan reached %d units for a one-client technician, want at most 1 (their own)", res.ScansSent)
	}
}

// 2.3-3's atomicity requirement, proven on the store: a refused bulk request
// writes nothing. The evidence is the store's own row counts and the rows'
// states before and after — not the response body.
func TestScopeBulkRefusalWritesNothing(t *testing.T) {
	f := seedFleetFixture(t)

	countDeployments := func() int64 {
		n, err := f.st.PatchDeployments.Count()
		if err != nil {
			t.Fatalf("count deployments: %v", err)
		}
		return n
	}
	before := countDeployments()

	// A deploy naming a foreign unit: refused, and no deployment row appears.
	if code, _, _ := postJSON(t, f.baseURL+"/api/patch/deploy", f.makeUserWithScope(t, "atomic-deploy", "manager", f.alice.ID), map[string]any{
		"device_ids": []string{f.inAlice, f.inBob},
	}); code != http.StatusBadRequest {
		t.Fatalf("mixed-scope deploy = %d, want 400", code)
	}
	if after := countDeployments(); after != before {
		t.Fatalf("the refused deploy created %d deployment row(s): %d -> %d", after-before, before, after)
	}

	// A bulk tag call naming a foreign unit: refused, and no unit's tags moved.
	alphaBefore, err := f.st.Devices.GetByID(f.inAlice)
	if err != nil {
		t.Fatalf("load alpha: %v", err)
	}
	betaBefore, err := f.st.Devices.GetByID(f.inBob)
	if err != nil {
		t.Fatalf("load beta: %v", err)
	}
	if code, _, _ := postJSON(t, f.baseURL+"/api/devices/tags/bulk", f.makeUserWithScope(t, "atomic-tags", "manager", f.alice.ID), map[string]any{
		"device_ids": []string{f.inAlice, f.inBob},
		"add":        []string{"squad-a"},
	}); code != http.StatusBadRequest {
		t.Fatalf("mixed-scope bulk tags = %d, want 400", code)
	}
	alphaAfter, _ := f.st.Devices.GetByID(f.inAlice)
	betaAfter, _ := f.st.Devices.GetByID(f.inBob)
	if len(alphaAfter.Tags) != len(alphaBefore.Tags) || len(betaAfter.Tags) != len(betaBefore.Tags) {
		t.Fatalf("the refused bulk tag call moved tags: alpha %v -> %v, beta %v -> %v",
			alphaBefore.Tags, alphaAfter.Tags, betaBefore.Tags, betaAfter.Tags)
	}

	// A patch scan naming a foreign unit: refused, and no scan was queued. The
	// scan queue's evidence is the update rows' state: a queued scan marks
	// nothing, so assert the response is a refusal and no new update rows appeared.
	updatesBefore, err := f.st.SoftwareUpdates.Count()
	if err != nil {
		t.Fatalf("count updates: %v", err)
	}
	if code, _, _ := postJSON(t, f.baseURL+"/api/patch/scan", f.makeUserWithScope(t, "atomic-scan", "technician", f.alice.ID), map[string]any{
		"device_ids": []string{f.inBob},
	}); code != http.StatusBadRequest {
		t.Fatalf("foreign-scope scan = %d, want 400", code)
	}
	if updatesAfter, _ := f.st.SoftwareUpdates.Count(); updatesAfter != updatesBefore {
		t.Fatalf("the refused scan changed the update rows: %d -> %d", updatesBefore, updatesAfter)
	}

	// A session start on a foreign unit is refused before any session row exists.
	sessionsBefore, err := f.st.Sessions.Count()
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if code, _, _ := postJSON(t, f.baseURL+"/api/devices/"+f.inBob+"/sessions", f.makeUserWithScope(t, "atomic-session", "technician", f.alice.ID), map[string]any{}); code != http.StatusNotFound {
		t.Fatalf("foreign session start = %d, want 404", code)
	}
	if sessionsAfter, _ := f.st.Sessions.Count(); sessionsAfter != sessionsBefore {
		t.Fatalf("the refused session start created %d session row(s)", sessionsAfter-sessionsBefore)
	}
}

// The role matrix the acceptance names: every caller shape × the read surfaces,
// asserting in-scope rows present, out-of-scope rows absent, and the scope the
// response names.
//
// The fixture makes the two compliance numbers **differ on purpose** — client A
// has two critical updates outstanding and client B has none — because a fleet
// view computed over the installation and one computed inside the scope look
// identical when the fleet is uniform. The test asserts the scoped percentages
// are the caller's, not the installation's.
func TestScopeRoleMatrixOnEveryReadSurface(t *testing.T) {
	f := seedFleetFixture(t)

	// Client A's unit carries two critical updates, client B's unit is clean.
	for i := 0; i < 2; i++ {
		if err := f.st.SoftwareUpdates.Create(&models.SoftwareUpdate{
			ID: uuid.NewString(), DeviceID: f.inAlice, Source: "apt",
			Title: fmt.Sprintf("critical-%d", i), ExternalID: uuid.NewString(),
			Severity: "critical", Status: "detected",
		}); err != nil {
			t.Fatalf("seed critical update: %v", err)
		}
	}
	// A policy aimed at client B's unit alone: invisible to A's manager, who
	// must not learn B's naming; visible to admin.
	policyB := &models.PatchPolicy{
		ID: uuid.NewString(), Name: "beta-nightly", Scope: "devices",
		ScopeValue: f.inBob, Schedule: "weekly",
	}
	if err := f.st.PatchPolicies.Create(policyB); err != nil {
		t.Fatalf("create policy: %v", err)
	}
	// A tag that exists only on client B's unit.
	bob, err := f.st.Devices.GetByID(f.inBob)
	if err != nil {
		t.Fatalf("load beta unit: %v", err)
	}
	bob.Tags = []string{"beta-only"}
	if err := f.st.Devices.Update(bob); err != nil {
		t.Fatalf("tag beta unit: %v", err)
	}

	// Client A's unit carries three open updates (one from `seedFleetFixture`,
	// two added below), client B's unit carries one, and the unassigned unit
	// carries none — so the installation's total (4) and A's total (3) differ,
	// which is the whole point of the fixture.
	cases := []struct {
		role        string
		scopeToA    bool
		wantUnits   int
		wantHeader  string
		wantPending int
	}{
		{role: "admin", scopeToA: false, wantUnits: 3, wantHeader: "all", wantPending: 4},
		{role: "manager", scopeToA: true, wantUnits: 1, wantHeader: "alice", wantPending: 3},
		{role: "technician", scopeToA: true, wantUnits: 1, wantHeader: "alice", wantPending: 3},
		{role: "viewer", scopeToA: true, wantUnits: 1, wantHeader: "alice", wantPending: 3},
		{role: "technician", scopeToA: false, wantUnits: 0, wantHeader: "none", wantPending: 0},
	}

	for i, tc := range cases {
		name := tc.role
		if !tc.scopeToA {
			name += "-unscoped"
		}
		t.Run(name, func(t *testing.T) {
			user := fmt.Sprintf("matrix2-%d", i)
			var token string
			if tc.scopeToA {
				token = f.makeUserWithScope(t, user, tc.role, f.alice.ID)
			} else {
				token = f.makeUserWithScope(t, user, tc.role)
			}

			// Fleet list.
			code, body, header := f.get(t, "/api/devices", token)
			if code != http.StatusOK {
				t.Fatalf("devices = %d, want 200", code)
			}
			var devEnv struct {
				Devices []models.Device `json:"devices"`
			}
			if err := json.Unmarshal(body, &devEnv); err != nil {
				t.Fatalf("decode devices: %v", err)
			}
			if len(devEnv.Devices) != tc.wantUnits {
				t.Fatalf("%s/%s: %d units, want %d", tc.role, name, len(devEnv.Devices), tc.wantUnits)
			}
			wantHeader := tc.wantHeader
			if wantHeader == "alice" {
				wantHeader = f.alice.ID
			}
			if header != wantHeader {
				t.Fatalf("%s: scope header = %q, want %q", name, header, wantHeader)
			}
			if !tc.scopeToA && tc.role != "admin" {
				for _, d := range devEnv.Devices {
					if d.ID == f.inBob {
						t.Fatalf("%s: the fleet list carried client B's unit", name)
					}
				}
			}

			// By-ID: in-scope unit answers, foreign unit answers the same 404
			// a nonexistent id answers.
			if tc.wantUnits > 0 {
				if code, body, _ := f.get(t, "/api/devices/"+f.inAlice, token); code != http.StatusOK {
					t.Fatalf("%s: in-scope unit = %d: %s", name, code, body)
				}
			}
			// The 404-shape assertion belongs to scoped callers: admin is
			// scope-all, so client B's unit is legitimately theirs.
			if tc.role != "admin" {
				_, missingBody, _ := f.get(t, "/api/devices/"+uuid.NewString(), token)
				foreignCode, foreignBody, _ := f.get(t, "/api/devices/"+f.inBob, token)
				if foreignCode != http.StatusNotFound || !bytes.Equal(missingBody, foreignBody) {
					t.Fatalf("%s: foreign unit = %d %s, want 404 identical to %s", name, foreignCode, foreignBody, missingBody)
				}
			}

			// Patch overview: the caller's compliance, computed inside the scope.
			code, body, _ = f.get(t, "/api/patch/overview", token)
			if code != http.StatusOK {
				t.Fatalf("%s: overview = %d, want 200", name, code)
			}
			var overview struct {
				Totals struct {
					Devices   int `json:"devices"`
					Compliant int `json:"compliant"`
					Pending   int `json:"pending"`
					Critical  int `json:"critical"`
				} `json:"totals"`
			}
			if err := json.Unmarshal(body, &overview); err != nil {
				t.Fatalf("decode overview: %v", err)
			}
			if overview.Totals.Devices != tc.wantUnits {
				t.Fatalf("%s: overview counts %d units, want %d", name, overview.Totals.Devices, tc.wantUnits)
			}
			if overview.Totals.Pending != tc.wantPending {
				t.Fatalf("%s: overview pending = %d, want %d (the scoped percentage is the caller's, not the installation's)",
					name, overview.Totals.Pending, tc.wantPending)
			}

			// Tags: the vocabulary of the caller's fleet only.
			code, body, _ = f.get(t, "/api/tags", token)
			if code != http.StatusOK {
				t.Fatalf("%s: tags = %d, want 200", name, code)
			}
			var tagEnv struct {
				Tags []struct {
					Tag   string `json:"tag"`
					Count int    `json:"count"`
				} `json:"tags"`
			}
			if err := json.Unmarshal(body, &tagEnv); err != nil {
				t.Fatalf("decode tags: %v", err)
			}
			if tc.role != "admin" {
				for _, tg := range tagEnv.Tags {
					if tg.Tag == "beta-only" {
						t.Fatalf("%s: the tag vocabulary disclosed client B's unit (%+v)", name, tagEnv.Tags)
					}
				}
			}

			// Policies: only those that can reach the caller's units.
			code, body, _ = f.get(t, "/api/patch/policies", token)
			if code != http.StatusOK {
				t.Fatalf("%s: policies = %d, want 200", name, code)
			}
			var polEnv struct {
				Policies []models.PatchPolicy `json:"policies"`
			}
			if err := json.Unmarshal(body, &polEnv); err != nil {
				t.Fatalf("decode policies: %v", err)
			}
			for _, p := range polEnv.Policies {
				if p.ID == policyB.ID && tc.role != "admin" {
					t.Fatalf("%s: a policy aimed at client B's unit was listed (%s)", name, p.Name)
				}
			}
			if tc.role == "admin" {
				found := false
				for _, p := range polEnv.Policies {
					if p.ID == policyB.ID {
						found = true
					}
				}
				if !found {
					t.Fatal("admin did not see the client B policy")
				}
			}

			// Sessions: the intersection of the owner rule and client scope —
			// a scoped caller never sees a foreign unit's session.
			code, body, _ = f.get(t, "/api/sessions", token)
			if code != http.StatusOK {
				t.Fatalf("%s: sessions = %d, want 200", name, code)
			}
			var sessionEnv struct {
				Sessions []models.Session `json:"sessions"`
			}
			if err := json.Unmarshal(body, &sessionEnv); err != nil {
				t.Fatalf("decode sessions: %v", err)
			}
			for _, sess := range sessionEnv.Sessions {
				if sess.DeviceID == f.inBob && tc.role != "admin" {
					t.Fatalf("%s: saw a session on client B's unit", name)
				}
			}
		})
	}
}

// Admin keeps the unrestricted view on the fleet surfaces too.
func TestScopeAdminFleetListsSeeEverything(t *testing.T) {
	f := seedFleetFixture(t)
	token := f.makeUserWithScope(t, "fleet-boss", "admin")

	_, body, header := f.get(t, "/api/patch/overview", token)
	if header != "all" {
		t.Fatalf("admin overview header = %q, want all", header)
	}
	var overview struct {
		Devices []struct {
			DeviceID string `json:"device_id"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(body, &overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if len(overview.Devices) != 3 {
		t.Fatalf("admin overview rows = %d, want all 3 units", len(overview.Devices))
	}

	code, _, _ := f.get(t, "/api/patch/deployments/"+f.extra["deployment"]+"/results", token)
	if code != http.StatusOK {
		t.Fatalf("admin deployment results = %d, want 200", code)
	}
}
