package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"

	"gorm.io/driver/sqlite"
)

// 2.3-6 — the audit ledger.

const auditTestPassword = "Passw0rd-2.3!"

//
// The rule this file defends is the product rule from the plan: **every
// mutating route records its actor, target, and outcome, and a refusal records
// too.** The point is not compliance theatre — an MSP answering "who rebooted
// the finance machine, and did it work?" needs the row to exist for the attempt
// that failed as much as for the one that succeeded.
//
// Two things make this testable as a gate rather than a wish: the router can
// enumerate itself, and the audit registry is one table. So the first test
// below compares the two, and a new mutating route cannot arrive without
// either an action name or a written reason for not having one.

// auditedSkip lists the mutating routes deliberately outside the ledger, with
// the reason. A route here is a decision, and the reason beside it is its
// record — the coverage test's value is that an unregistered route cannot slip
// past unnoticed, so a bare entry is itself a failure.
//
// Everything on the list is agent-side: the actor is a device key, not a
// person, so a row would name a machine as the author of a human act. Those
// writes are recorded where they belong — as device reports, deployment
// results, and session rows.
var auditedSkip = map[string]string{
	// Agent-side writes: the actor is a device key, not a person. A row for
	// these would name a machine as the author of a human act; they are
	// recorded where they belong, as device reports, deployment results, and
	// session rows.
	"POST /api/agent/register":                  "device-key auth: the device enrols itself",
	"POST /api/agent/heartbeat":                 "device-key auth: telemetry",
	"POST /api/agent/metrics":                   "device-key auth: telemetry",
	"POST /api/agent/updates":                   "device-key auth: the agent reporting, not a technician acting",
	"POST /api/agent/deployments/result":        "device-key auth: the agent reporting, not a technician acting",
	"POST /api/agent/files/:transfer_id/upload": "device-key auth: the agent uploading a file it was asked to pull",
	"POST /api/agent/files/status":              "device-key auth: the agent reporting a transfer's status",
	"POST /api/sessions/:id/frame":              "the agent reporting a screen frame; the session row is the record",

	// Token mechanics: the console calls these on every boot and on every
	// SSO handshake, so the ledger would be a log of page loads.
	"POST /api/auth/refresh":      "token mechanics: a client refreshing its access token",
	"POST /api/auth/sso/exchange": "token mechanics: the SSO handshake exchanging a code",
}

// TestEveryMutatingRouteIsAudited is 2.3-6's coverage rule as an executable:
// enumerate the live router, keep the mutating verbs, and fail on any route
// with no audit action and no written reason.
func TestEveryMutatingRouteIsAudited(t *testing.T) {
	engine := newAuditRouteEnumerator(t)

	mutating := map[string]bool{}
	for _, r := range engine.Routes() {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			continue
		}
		if strings.HasPrefix(r.Path, "/ws") {
			continue // streams are not acts
		}
		mutating[r.Method+" "+r.Path] = true
	}
	if len(mutating) < 40 {
		t.Fatalf("enumerated only %d mutating routes; the router is not fully built", len(mutating))
	}

	var missing []string
	for key := range mutating {
		if _, ok := auditedSkip[key]; ok {
			continue
		}
		method, path, _ := strings.Cut(key, " ")
		if _, ok := lookupAuditAction(method, path); !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Errorf("mutating routes with no audit action and no skip reason:\n  %s",
			strings.Join(missing, "\n  "))
	}

	// A skip entry for a route the router does not have is a claim about the
	// product that stopped being true: it must fail, so the list cannot rot.
	for key := range auditedSkip {
		if !mutating[key] {
			t.Errorf("skip entry %q names no mutating route of this router", key)
		}
	}
}

// newAuditRouteEnumerator builds the real router without serving it, so the
// route table is the production one.
func newAuditRouteEnumerator(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	jwtAuth := auth.NewJWTAuth("test-secret-key")
	hub := ws.NewHub("http://localhost:3000", "")
	engine := alerts.NewEngine(st.Alerts)
	return SetupRouter(context.Background(), st, jwtAuth, hub, engine, "http://localhost:3000", "", 100, 0)
}

// TestAuditRecordsActorTargetOutcome: a technician deletes a unit and the row
// names the technician, the unit, the tenant, and the outcome.
func TestAuditRecordsActorTargetOutcome(t *testing.T) {
	f := newScopeFixture(t)
	token := f.makeUserWithScope(t, "audit-owner", "admin")
	code, resp, _ := f.do(t, http.MethodDelete, f.baseURL+"/api/devices/"+f.inAlice, token, nil)
	if code != http.StatusOK {
		t.Fatalf("delete = %d %s", code, resp)
	}

	// The login that produced the token is a row too — that is the point of
	// recording logins — so the assertion walks for the deletion.
	rows := auditRows(t, f.st, 20)
	var row models.AuditLog
	var deletes int
	for _, r := range rows {
		if r.Action == models.AuditDeviceDelete {
			deletes++
			row = r
		}
	}
	if deletes != 1 {
		t.Fatalf("device.delete rows = %d, want exactly one\n%s", deletes, dumpAuditRows(rows))
	}
	var loggedLogin bool
	for _, r := range rows {
		if r.Action == models.AuditAuthLogin && r.Outcome == models.AuditOutcomeOK {
			loggedLogin = true
		}
	}
	if !loggedLogin {
		t.Error("the login that authorised the act is not in the ledger")
	}
	if row.Action != models.AuditDeviceDelete {
		t.Errorf("Action = %q, want %q", row.Action, models.AuditDeviceDelete)
	}
	if row.ActorUsername != "audit-owner" || row.ActorUserID == "" {
		t.Errorf("actor = %q/%q: the record must name who acted", row.ActorUserID, row.ActorUsername)
	}
	if !strings.Contains(row.ActorRoles, "admin") {
		t.Errorf("ActorRoles = %q: the record must carry the roles at the time of the act", row.ActorRoles)
	}
	if row.TargetType != "device" || row.TargetID != f.inAlice {
		t.Errorf("target = %s/%s, want device/%s", row.TargetType, row.TargetID, f.inAlice)
	}
	if row.ClientID != f.alice.ID {
		t.Errorf("ClientID = %q, want the unit's client at the time of the act", row.ClientID)
	}
	if row.Outcome != models.AuditOutcomeOK {
		t.Errorf("Outcome = %q, want ok", row.Outcome)
	}
	if row.ID == "" || row.SourceIP == "" {
		t.Errorf("row lacks its id or source ip: %#v", row)
	}
	if !json.Valid([]byte(row.Details)) {
		t.Errorf("details are not valid JSON: %q", row.Details)
	}
}

// TestAuditRecordsRefusals: an out-of-scope attempt is recorded. The 404 is
// what the caller sees; the row is what the MSP sees.
func TestAuditRecordsRefusals(t *testing.T) {
	f := newScopeFixture(t)
	// A manager scoped to Alice deleting Bob's unit. Deleting a unit is an
	// admin act, so the RBAC middleware answers first — 403, before any handler
	// runs — and the middleware's hook is what records it. A ledger that only
	// the handlers wrote would have no denied attempts in it at all, and denied
	// attempts are what an MSP reads the log for.
	token := f.makeUserWithScope(t, "audit-tech", "manager", f.alice.ID)
	code, _, _ := f.do(t, http.MethodDelete, f.baseURL+"/api/devices/"+f.inBob, token, nil)
	if code != http.StatusForbidden {
		t.Fatalf("manager delete = %d, want 403 (RBAC answers before scope, and before any handler)", code)
	}

	rows := auditRows(t, f.st, 10)
	if len(rows) == 0 {
		t.Fatal("nothing recorded: a refusal must be a row")
	}
	var found bool
	for _, row := range rows {
		if row.Action != models.AuditDeviceDelete {
			continue
		}
		found = true
		if row.Outcome != models.AuditOutcomeRefused {
			t.Errorf("Outcome = %q, want refused", row.Outcome)
		}
		if row.ActorUsername != "audit-tech" {
			t.Errorf("actor = %q, want the technician who attempted it", row.ActorUsername)
		}
		if !strings.Contains(row.Details, f.inBob) {
			t.Errorf("details = %q, want the unit the attempt named", row.Details)
		}
	}
	if !found {
		t.Error("no device.delete row for the attempt\n" + dumpAuditRows(rows))
	}
}

// TestAuditRecordsLoginOutcomes: a login system's log is mostly failures, so a
// ledger with only successes hides the thing an MSP asks about.
func TestAuditRecordsLoginOutcomes(t *testing.T) {
	f := newScopeFixture(t)
	f.makeUserWithScope(t, "audit-login", "viewer")

	code, _, _ := f.do(t, http.MethodPost, f.baseURL+"/api/auth/login", "", map[string]any{
		"username": "audit-login",
		"password": "not-the-password",
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", code)
	}

	var sawFailure, sawSuccess bool
	for _, row := range auditRows(t, f.st, 10) {
		if row.Action == models.AuditAuthLoginFailed {
			sawFailure = true
			if row.Outcome != models.AuditOutcomeRefused {
				t.Errorf("failure Outcome = %q, want refused", row.Outcome)
			}
			if row.ActorUsername != "audit-login" {
				t.Errorf("failure actor = %q, want the account that was tried", row.ActorUsername)
			}
			if strings.Contains(row.Details, "not-the-password") {
				t.Error("the record contains the password that was tried")
			}
		}
	}

	code, body, _ := f.do(t, http.MethodPost, f.baseURL+"/api/auth/login", "", map[string]any{
		"username": "audit-login",
		"password": "Passw0rd-2.3!",
	})
	if code != http.StatusOK {
		t.Fatalf("login = %d, want 200: %s", code, body)
	}
	for _, row := range auditRows(t, f.st, 20) {
		if row.Action == models.AuditAuthLogin && row.Outcome == models.AuditOutcomeOK {
			sawSuccess = true
			if row.ActorUserID == "" {
				t.Error("the successful login does not name the account")
			}
			if !strings.Contains(row.ActorRoles, "viewer") {
				t.Errorf("roles = %q, want the roles the token carries", row.ActorRoles)
			}
		}
	}
	if !sawFailure {
		t.Error("the failed login was not recorded")
	}
	if !sawSuccess {
		t.Error("the successful login was not recorded")
	}
}

// TestAuditBulkRefusalNamesTargets: a refused bulk command records the ids it
// refused — "who tried to deploy to a unit that is not theirs" has to be
// answerable from the row.
func TestAuditBulkRefusalNamesTargets(t *testing.T) {
	f := newScopeFixture(t)
	token := f.makeUserWithScope(t, "audit-bulk", "manager", f.alice.ID)

	code, body, _ := f.do(t, http.MethodPost, f.baseURL+"/api/devices/tags/bulk", token, map[string]any{
		"device_ids": []string{f.inBob},
		"add":        []string{"staging"},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("bulk = %d %s, want 400", code, body)
	}

	var found bool
	for _, row := range auditRows(t, f.st, 10) {
		if row.Action != models.AuditDeviceTags {
			continue
		}
		found = true
		if row.Outcome != models.AuditOutcomeRefused {
			t.Errorf("Outcome = %q, want refused", row.Outcome)
		}
		if !strings.Contains(row.Details, f.inBob) {
			t.Errorf("details = %q, want the refused unit id", row.Details)
		}
	}
	if !found {
		t.Error("no device.tags row for the refused bulk command")
	}
}

// TestAuditRecordsSessionActs: starting and ending a remote session are the
// most sensitive acts in the product, so both are rows.
func TestAuditRecordsSessionActs(t *testing.T) {
	f := newScopeFixture(t)
	token := f.makeUserWithScope(t, "audit-session", "technician", f.alice.ID)

	// The unit is offline in this fixture, so the start is refused — and the
	// refusal is the point: a session attempt at a tenant's machine is worth a
	// row whether or not it worked.
	code, _, _ := f.do(t, http.MethodPost, f.baseURL+"/api/devices/"+f.inAlice+"/sessions", token, map[string]any{})
	if code == http.StatusOK {
		t.Skip("fixture unit accepted a session; the refusal assertion below needs the offline path")
	}

	var found bool
	for _, row := range auditRows(t, f.st, 10) {
		if row.Action != models.AuditSessionStart {
			continue
		}
		found = true
		if row.ActorUsername != "audit-session" {
			t.Errorf("actor = %q, want the technician who tried", row.ActorUsername)
		}
		if row.TargetType != "device" || row.TargetID != f.inAlice {
			t.Errorf("target = %s/%s, want the unit the attempt named", row.TargetType, row.TargetID)
		}
	}
	if !found {
		t.Error("the session attempt was not recorded")
	}
}

// TestAuditLedgerEndpoint: the console reads the ledger through an endpoint
// that admins scope fully and managers see only for their clients.
func TestAuditLedgerEndpoint(t *testing.T) {
	f := newScopeFixture(t)
	if err := f.st.Audit.Record(&models.AuditLog{
		ID: models.NewAuditID(), Action: models.AuditDeviceDelete,
		TargetType: "device", TargetID: f.inAlice, ClientID: f.alice.ID,
		Outcome: models.AuditOutcomeOK, ActorUsername: "someone",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := f.st.Audit.Record(&models.AuditLog{
		ID: models.NewAuditID(), Action: models.AuditDeviceDelete,
		TargetType: "device", TargetID: f.inBob, ClientID: f.bob.ID,
		Outcome: models.AuditOutcomeOK, ActorUsername: "someone-else",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	admin := f.makeUserWithScope(t, "audit-admin", "admin")
	code, body, header := f.do(t, http.MethodGet, f.baseURL+"/api/audit", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("admin ledger = %d %s", code, body)
	}
	if header == "" {
		t.Error("the ledger response carries no scope header")
	}
	var page struct {
		Logs      []models.AuditLog `json:"logs"`
		Scope     string            `json:"scope"`
		Total     int               `json:"total"`
		Retention int               `json:"retention_days"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode ledger: %v\n%s", err, body)
	}
	// The fixture's own logins are rows too, so the assertion counts the seeded
	// pair: the admin's page is the one place rows from a tenant the caller
	// has no grant for appear.
	if seeded := countSeeded(page.Logs); seeded != 2 {
		t.Errorf("admin sees %d of the two seeded rows, want both\n%s", seeded, dumpAuditRows(page.Logs))
	}
	if page.Scope != "all" {
		t.Errorf("scope = %q, want all", page.Scope)
	}
	if page.Retention <= 0 {
		t.Errorf("retention_days = %d: the endpoint must state the retention it applies", page.Retention)
	}

	tech := f.makeUserWithScope(t, "audit-scoped", "manager", f.alice.ID)
	code, body, _ = f.do(t, http.MethodGet, f.baseURL+"/api/audit", tech, nil)
	if code != http.StatusOK {
		t.Fatalf("manager ledger = %d %s", code, body)
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode manager ledger: %v", err)
	}
	if seeded := countSeeded(page.Logs); seeded != 1 || page.Logs[0].ClientID != f.alice.ID {
		t.Errorf("manager sees %d of the seeded rows, want only their client's: %s", seeded, dumpAuditRows(page.Logs))
	}

	viewer := f.makeUserWithScope(t, "audit-viewer", "viewer")
	code, _, _ = f.do(t, http.MethodGet, f.baseURL+"/api/audit", viewer, nil)
	if code != http.StatusForbidden {
		t.Errorf("viewer ledger = %d, want 403: the ledger is an operator surface", code)
	}

	// Filters reach the query.
	code, body, _ = f.do(t, http.MethodGet, f.baseURL+"/api/audit?action="+models.AuditSessionStart, admin, nil)
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode filtered ledger: %v", err)
	}
	if code != http.StatusOK || len(page.Logs) != 0 {
		t.Errorf("action filter returned %d rows, want none", len(page.Logs))
	}
}

// countSeeded counts the rows the ledger fixture wrote by hand, identified by
// the actor names it gave them: the fixture's logins are legitimate rows and
// must not be excluded from the page, only from the count.
func countSeeded(rows []models.AuditLog) int {
	var n int
	for _, r := range rows {
		if r.ActorUsername == "someone" || r.ActorUsername == "someone-else" {
			n++
		}
	}
	return n
}

// TestAuditRowsAreWrittenBeforeTheResponse: the record describes the act, not
// the successful end of the request. Here the act **fails** — the installation
// wipes its monitoring history and the store refuses the write — and the row
// is still there, carrying the failure. A writer hooked to the success path
// loses exactly the events someone is reading the log for, and that is the
// reason the writer is deferred at the top of the handler rather than placed
// above the 200.
func TestAuditRowsAreWrittenBeforeTheResponse(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "audit-order", "admin")

	// Break the write, not the request: the history table is dropped, so the
	// handler runs, its first store call fails, and the caller gets a 500.
	if err := f.st.DB.Migrator().DropTable(&models.MetricHistory{}); err != nil {
		t.Fatalf("drop metric history: %v", err)
	}
	code, _, _ := f.do(t, http.MethodDelete, f.baseURL+"/api/monitoring/data", admin, nil)
	if code != http.StatusInternalServerError {
		t.Fatalf("clear monitoring data = %d, want 500 (the fixture must produce a failing act)", code)
	}

	var found bool
	for _, row := range auditRows(t, f.st, 20) {
		if row.Action != models.AuditMonitoringPurge {
			continue
		}
		found = true
		if row.Outcome != models.AuditOutcomeError {
			t.Errorf("Outcome = %q, want error: the record must carry the answer the caller got", row.Outcome)
		}
		if row.ActorUsername != "audit-order" {
			t.Errorf("actor = %q, want the technician who pressed it", row.ActorUsername)
		}
		if row.TargetType != "monitoring" {
			t.Errorf("target = %q, want the whole history this act pointed at", row.TargetType)
		}
	}
	if !found {
		t.Error("a failed act left no record")
	}
}

// TestAuditRecordsTheAccountProvider: the provider is stored on the row,
// because providers are deleted and an account's origin is part of the record.
func TestAuditRecordsTheAccountProvider(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "audit-provider", "admin")

	code, _, _ := f.do(t, http.MethodDelete, f.baseURL+"/api/devices/"+f.inAlice, admin, nil)
	if code != http.StatusOK {
		t.Fatalf("delete = %d", code)
	}

	var found bool
	for _, row := range auditRows(t, f.st, 20) {
		if row.Action != models.AuditDeviceDelete {
			continue
		}
		found = true
		if row.ActorProvider != "local" {
			t.Errorf("ActorProvider = %q, want the account's provider (the fixture's users are local)", row.ActorProvider)
		}
	}
	if !found {
		t.Error("no device.delete row")
	}
}

// TestAuditCSVExport: an MSP hands the log to a client, so the export is a
// file with the record's columns, not a JSON dump renamed.
func TestAuditCSVExport(t *testing.T) {
	f := newScopeFixture(t)
	if err := f.st.Audit.Record(&models.AuditLog{
		ID: models.NewAuditID(), Action: models.AuditDeviceDelete,
		TargetType: "device", TargetID: f.inAlice, ClientID: f.alice.ID,
		Outcome: models.AuditOutcomeOK, ActorUsername: "seeded",
		ActorRoles: "admin", Details: `{"name":"unit-a"}`,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	admin := f.makeUserWithScope(t, "audit-csv", "admin")

	code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/audit?format=csv", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("csv export = %d %s", code, body)
	}
	text := string(body)
	if !strings.HasPrefix(text, "timestamp,actor,actor_roles,action,target_type,") {
		t.Errorf("csv header = %q", text[:min(len(text), 80)])
	}
	if !strings.Contains(text, "seeded,admin,device.delete") {
		t.Errorf("csv has no seeded row:\n%s", text)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	// Header plus the seeded row; the fixture's own logins are legitimate rows
	// and travel in the export too, so the assertion is a floor, not a count.
	if len(lines) < 2 {
		t.Errorf("csv has %d lines, want at least header + row", len(lines))
	}
	if strings.Contains(text, "viewer_token") || strings.Contains(text, "password") {
		t.Error("the export carries a secret")
	}
}

// TestAuditStatesRetention: the ledger page states the window it applies and
// the sweep that enforces it, so the promise is readable rather than assumed.
func TestAuditStatesRetention(t *testing.T) {
	f := newScopeFixture(t)
	SetAuditRetention(45)
	t.Cleanup(func() { SetAuditRetention(90) })

	admin := f.makeUserWithScope(t, "audit-retention", "admin")
	code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/audit", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("ledger = %d %s", code, body)
	}
	var page struct {
		Logs      []models.AuditLog `json:"logs"`
		Retention struct {
			Days        int       `json:"days"`
			RowsPruned  int64     `json:"rows_pruned"`
			LastRun     time.Time `json:"last_run"`
			SweepFailed bool      `json:"sweep_errored"`
		} `json:"retention"`
		RetentionDays int `json:"retention_days"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	if page.RetentionDays != 45 || page.Retention.Days != 45 {
		t.Errorf("retention = %d/%d, want the configured 45 days stated in both places",
			page.RetentionDays, page.Retention.Days)
	}

	// The sweep's counters appear once a sweeper is registered.
	SetRetentionSweeper(fakeSweeper{pruned: 7, ranAt: time.Unix(1700000000, 0)})
	t.Cleanup(func() { SetRetentionSweeper(nil) })
	_, body, _ = f.do(t, http.MethodGet, f.baseURL+"/api/audit", admin, nil)
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode with sweeper: %v", err)
	}
	if page.Retention.RowsPruned != 7 || page.Retention.LastRun.Unix() != 1700000000 {
		t.Errorf("retention counters = %+v, want the sweep that ran", page.Retention)
	}
}

type fakeSweeper struct {
	pruned int64
	ranAt  time.Time
}

func (s fakeSweeper) RetentionSummary() (int64, time.Time, bool) {
	return s.pruned, s.ranAt, false
}

// TestAuditRetentionPrunes: retention is a promise with a number on it.
func TestAuditRetentionPrunes(t *testing.T) {
	f := newScopeFixture(t)
	for i := 0; i < 3; i++ {
		if err := f.st.Audit.Record(&models.AuditLog{
			ID: models.NewAuditID(), Action: models.AuditDeviceDelete,
			TargetType: "device", TargetID: uuid.NewString(),
			Outcome: models.AuditOutcomeOK,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if n := f.st.Audit.Count(); n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
	// Every row is stamped now, so a cutoff in the past keeps them and a
	// cutoff in the future prunes them: both sides of the boundary are
	// asserted, because retention that only ever deletes is as broken as
	// retention that never deletes.
	n, err := f.st.Audit.PruneOlderThan(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 0 {
		t.Errorf("pruned %d rows newer than the cutoff, want 0", n)
	}
	n, err = f.st.Audit.PruneOlderThan(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 3 {
		t.Errorf("pruned %d, want 3", n)
	}
	if n := f.st.Audit.Count(); n != 0 {
		t.Errorf("count after prune = %d, want 0", n)
	}
}

func auditRows(t *testing.T, st *store.Store, limit int) []models.AuditLog {
	t.Helper()
	rows, err := st.Audit.List(store.AuditFilter{Limit: limit})
	if err != nil {
		t.Fatalf("Audit.List: %v", err)
	}
	return rows
}

// seedDevice adds a unit to the fixture fleet with the given name, unassigned
// to any client.
func seedDevice(t *testing.T, st *store.Store, name string) *models.Device {
	t.Helper()
	device := &models.Device{
		ID: uuid.NewString(), Name: name, Hostname: name, OS: "linux", Arch: "amd64",
		Status: "online", Tags: []string{}, DeviceKey: uuid.NewString(),
	}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return device
}

func dumpAuditRows(rows []models.AuditLog) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strings.Join([]string{r.Action, r.ActorUsername, r.TargetType, r.TargetID, r.Outcome, r.ClientID}, " "))
		b.WriteString("\n")
	}
	return b.String()
}

// TestAuditRecordsClientGrant closes 2.3-1's open criterion: **the act that
// changes what an account can see is itself a record**.
//
// Grants are the most consequential writes in an MSP installation — they decide
// which technician can see which customer's machines — and they are the writes
// with no unit, no reboot and no visible side effect to notice them by. A
// support thread that starts "why can't Sam see the North Grid units?" is
// answered by this row: who granted, to which tenant, which accounts the grant
// replaced, and what the server answered.
func TestAuditRecordsClientGrant(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "grant-audit-admin", "admin")
	f.makeUserWithScope(t, "grant-audit-tech", "technician")
	tech, err := f.st.Users.GetByUsername("grant-audit-tech")
	if err != nil {
		t.Fatalf("load the technician: %v", err)
	}

	grantPath := "/api/clients/" + f.alice.ID + "/users"
	code, body, _ := f.do(t, http.MethodPut, f.baseURL+grantPath, admin, map[string]any{
		"user_ids": []string{tech.ID},
	})
	if code != http.StatusOK {
		t.Fatalf("grant = %d %s", code, body)
	}

	var grant *models.AuditLog
	for _, r := range auditRows(t, f.st, 30) {
		if r.Action == models.AuditClientGrant && r.ActorUsername == "grant-audit-admin" {
			row := r
			grant = &row
		}
	}
	if grant == nil {
		t.Fatalf("no client.grant row after a grant\n%s", dumpAuditRows(auditRows(t, f.st, 30)))
	}
	if grant.Outcome != models.AuditOutcomeOK {
		t.Errorf("grant row outcome = %q, want %q", grant.Outcome, models.AuditOutcomeOK)
	}
	// The target is the tenant whose access list changed, and the row carries
	// the tenant: a manager reading "who changed my client" must find the row
	// without joining through a device that has since moved.
	if grant.TargetType != "client" || grant.TargetID != f.alice.ID || grant.ClientID != f.alice.ID {
		t.Errorf("grant row target = %s/%s client %q, want client/%s client %s",
			grant.TargetType, grant.TargetID, grant.ClientID, f.alice.ID, f.alice.ID)
	}
	// The record says which account was granted and which ones it replaced.
	if !strings.Contains(grant.Details, tech.ID) {
		t.Errorf("grant row details do not name the granted account: %s", grant.Details)
	}
	if !strings.Contains(grant.Details, "was") {
		t.Errorf("grant row details do not state the grant set it replaced: %s", grant.Details)
	}

	// The refusal half. `PUT /api/clients/:id/users` is admin-only, and
	// `RequireAnyRole` aborts **before** any handler runs, so the handler-side
	// writer cannot see this attempt: the row exists only because the
	// middleware records it. A grant attempt by someone who cannot grant is
	// exactly the event an audit log is read for.
	techToken := loginUser(t, f.baseURL, "grant-audit-tech", "Passw0rd-2.3!")
	code, body, _ = f.do(t, http.MethodPut, f.baseURL+grantPath, techToken, map[string]any{
		"user_ids": []string{tech.ID},
	})
	if code != http.StatusForbidden {
		t.Fatalf("technician grant = %d, want 403 (%s)", code, body)
	}

	var refusal *models.AuditLog
	for _, r := range auditRows(t, f.st, 40) {
		if r.Action == models.AuditClientGrant && r.ActorUsername == "grant-audit-tech" {
			row := r
			refusal = &row
		}
	}
	if refusal == nil {
		t.Fatalf("a refused grant wrote no row\n%s", dumpAuditRows(auditRows(t, f.st, 40)))
	}
	if refusal.Outcome != models.AuditOutcomeRefused {
		t.Errorf("refusal outcome = %q, want %q", refusal.Outcome, models.AuditOutcomeRefused)
	}

	// And the grant the technician tried did not happen.
	code, users, _ := f.get(t, "/api/clients/"+f.alice.ID+"/users", techToken)
	if code == http.StatusOK {
		var env struct {
			Users []models.User `json:"users"`
		}
		_ = json.Unmarshal(users, &env)
		if len(env.Users) != 1 {
			t.Fatalf("the refused grant changed the access list: %+v", env.Users)
		}
	}

	// The tenant column is what makes these rows a *tenant's* record. The
	// ledger scopes a manager with `client_id IN (granted)`, so a grant row
	// with no tenant is readable by the admin alone — and a client's manager,
	// asking who changed who can see that client, is told nothing.
	granted := f.makeUserWithScope(t, "grant-audit-manager", "manager", f.alice.ID)
	 outsider := f.makeUserWithScope(t, "grant-audit-outsider", "manager", f.bob.ID)
	_, mine, _ := f.get(t, "/api/audit?action=client.grant", granted)
	_, theirs, _ := f.get(t, "/api/audit?action=client.grant", outsider)
	if !jsonHasArrayNamed(mine, "logs", 1) {
		t.Errorf("the granted client's manager reads no grant row for their own client: %s", mine)
	}
	if !jsonHasArrayNamed(theirs, "logs", 0) {
		t.Errorf("a manager of another tenant reads a grant row: %s", theirs)
	}
}
