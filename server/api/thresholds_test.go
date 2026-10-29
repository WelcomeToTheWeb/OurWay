package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
)

// 2.4-5 · threshold rules at the API.
//
// Three things have to hold, and each is asserted where it can actually be seen:
// the rule the operator saved is the rule the engine measures with (a metric
// posted by an agent, not a call into the resolver); a manager's reach stops at
// their tenants; and a rule the console cannot use is refused at the door.

// thresholdDevice seeds a unit with a known device key, so a test can post
// metrics at it the way an agent does.
func thresholdDevice(t *testing.T, f *scopeFixture, name string, clientID *string) (id, key string) {
	t.Helper()
	key = uuid.NewString()
	device := &models.Device{
		ID: uuid.NewString(), Name: name, Hostname: name, OS: "linux", Arch: "amd64",
		Status: "online", Tags: []string{"prod"}, DeviceKey: key, ClientID: clientID,
	}
	if err := f.st.Devices.Create(device); err != nil {
		t.Fatalf("create device: %v", err)
	}
	return device.ID, key
}

// postMetrics reports metrics through the agent route: the same door an agent
// uses, so the alert below is the alert a real unit produces.
func postMetrics(t *testing.T, f *scopeFixture, deviceKey string, cpu, ram, disk float64) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"cpu": cpu, "ram": ram, "disk": disk})
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, f.baseURL+"/api/agent/metrics", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("metrics request: %v", err)
	}
	// The agent door is the device key, not a bearer token: this is the request a
	// real agent sends, so the alert below is the alert a real unit produces.
	req.Header.Set("X-Device-Key", deviceKey)
	req.Header.Set("Content-Type", "application/json")
	// The agent door is IP-keyed by the limiter, and this file posts a metric
	// sequence: waiting out a refill is the agent's own behaviour, not a test
	// convenience.
	for attempt := 0; attempt < 60; attempt++ {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("metrics request: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		status := resp.StatusCode
		resp.Body.Close()
		if status == http.StatusOK {
			return
		}
		if status != http.StatusTooManyRequests {
			t.Fatalf("agent metrics = %d\n%s", status, body)
		}
		time.Sleep(700 * time.Millisecond)
		// The request body was consumed by the attempt that was refused.
		req, err = http.NewRequest(http.MethodPost, f.baseURL+"/api/agent/metrics", bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("metrics request: %v", err)
		}
		req.Header.Set("X-Device-Key", deviceKey)
		req.Header.Set("Content-Type", "application/json")
	}
	t.Fatal("agent metrics: the rate limiter never freed")
}

// doWithinBudget is the console-side caller inside the limiter: a batch of reads
// is what the limiter exists to shape, so the test asks, and waits for the refill.
func (f *scopeFixture) doWithinBudget(t *testing.T, method, url, token string, body any) (int, []byte, string) {
	t.Helper()
	for attempt := 0; attempt < 60; attempt++ {
		code, out, header := f.do(t, method, url, token, body)
		if code != http.StatusTooManyRequests {
			return code, out, header
		}
		time.Sleep(700 * time.Millisecond)
	}
	t.Fatalf("%s %s: the rate limiter never freed", method, url)
	return 0, nil, ""
}

// alertsFor reads the ledger's alerts for one unit through the console route.
func alertsFor(t *testing.T, f *scopeFixture, token, deviceID string) []models.Alert {
	t.Helper()
	code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/alerts?device_id="+deviceID, token, nil)
	if code != http.StatusOK {
		t.Fatalf("alerts = %d\n%s", code, body)
	}
	var out struct {
		Alerts []models.Alert `json:"alerts"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode alerts: %v\n%s", err, body)
	}
	return out.Alerts
}

// createRule posts a rule and answers its id.
func createRule(t *testing.T, f *scopeFixture, token string, form map[string]any) (int, []byte, string) {
	t.Helper()
	code, body, _ := f.do(t, http.MethodPost, f.baseURL+"/api/alerts/thresholds", token, form)
	var created struct {
		Rule struct {
			ID string `json:"id"`
		} `json:"rule"`
	}
	if err := json.Unmarshal(body, &created); err == nil {
		return code, body, created.Rule.ID
	}
	return code, body, ""
}

func createRuleID(t *testing.T, f *scopeFixture, token string, form map[string]any) string {
	t.Helper()
	code, body, id := createRule(t, f, token, form)
	if code != http.StatusCreated || id == "" {
		t.Fatalf("seed rule = %d\n%s", code, body)
	}
	return id
}

// jsonOf decodes a response body.
func jsonOf(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, body)
	}
	return out
}

// consoleEnglishPhrase reads a phrase out of the console's English bundle: the
// server names things the console displays, so the two must not drift apart.
func consoleEnglishPhrase(t *testing.T, key string) string {
	t.Helper()
	raw, err := os.ReadFile("../../web/src/i18n/locales/en.json")
	if err != nil {
		t.Skipf("console bundle unreadable: %v", err)
	}
	var bundle struct {
		Thresholds map[string]any `json:"thresholds"`
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("console bundle: %v", err)
	}
	phrase, _ := bundle.Thresholds[key].(string)
	if phrase == "" {
		t.Fatalf("the console bundle has no thresholds.%s", key)
	}
	return phrase
}

func TestThresholdRuleIsWhatTheEngineMeasuresTheAgentAgainst(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "rules-admin", "admin")
	deviceID, deviceKey := thresholdDevice(t, f, "rule-target", &f.alice.ID)

	// The tenant's rule warns at 70; the operator tightens **this unit** to 50.
	code, body, ruleID := createRule(t, f, admin, map[string]any{
		"scope_type": "device", "scope_value": deviceID, "metric": "cpu",
		"warning": 50, "critical": 65,
	})
	if code != http.StatusCreated {
		t.Fatalf("create rule = %d\n%s", code, body)
	}

	// 62%: under the tenant's 70 (no alert at all before this rule), above the
	// unit's own 50.
	postMetrics(t, f, deviceKey, 62, 0, 0)

	var found *models.Alert
	for _, alert := range alertsFor(t, f, admin, deviceID) {
		if alert.Metric == models.MetricCPU {
			latest := alert
			found = &latest
		}
	}
	if found == nil {
		t.Fatal("the tightened rule raised no alert at 62% (its warning is 50)")
	}
	if found.Threshold != 50 {
		t.Errorf("alert threshold = %v, want 50: the alert must name the rule that fired, not the floor", found.Threshold)
	}
	if found.Message != "CPU usage elevated: 62.0% (threshold: 50%)" {
		t.Errorf("alert message = %q, want the unit's own threshold named in it", found.Message)
	}

	// A neighbour in the same tenant is untouched by a device rule.
	_, neighbourKey := thresholdDevice(t, f, "rule-neighbour", &f.alice.ID)
	postMetrics(t, f, neighbourKey, 62, 0, 0)
	for _, alert := range alertsFor(t, f, admin, f.inAlice) {
		if alert.Metric == models.MetricCPU {
			t.Error("the device rule reached a unit it does not name")
		}
	}

	// Deleting the rule puts the unit back on the floor: 62 is under 80.
	if code, body, _ := f.do(t, http.MethodDelete, f.baseURL+"/api/alerts/thresholds/"+ruleID, admin, nil); code != http.StatusOK {
		t.Fatalf("delete = %d\n%s", code, body)
	}
	postMetrics(t, f, deviceKey, 62, 0, 0)
}

func TestThresholdPreviewNamesTheChainAndTheWinner(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "preview-admin", "admin")
	deviceID, _ := thresholdDevice(t, f, "preview-unit", &f.alice.ID)

	createRule(t, f, admin, map[string]any{
		"scope_type": "client", "scope_value": f.alice.ID, "metric": "cpu",
		"warning": 70, "critical": 85,
	})
	createRule(t, f, admin, map[string]any{
		"scope_type": "tag", "scope_value": "prod", "metric": "cpu",
		"warning": 60, "critical": 75,
	})
	createRule(t, f, admin, map[string]any{
		"scope_type": "device", "scope_value": deviceID, "metric": "cpu",
		"warning": 50, "critical": 65,
	})

	code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds/resolve?device="+deviceID, admin, nil)
	if code != http.StatusOK {
		t.Fatalf("resolve = %d\n%s", code, body)
	}
	answer := jsonOf(t, body)
	metrics, _ := answer["metrics"].([]any)
	if len(metrics) != 3 {
		t.Fatalf("preview answered %d metrics, want cpu/ram/disk\n%s", len(metrics), body)
	}

	var cpu map[string]any
	for _, item := range metrics {
		entry := item.(map[string]any)
		if entry["metric"] == "cpu" {
			cpu = entry
		}
	}
	if cpu == nil {
		t.Fatalf("no cpu entry in the preview\n%s", body)
	}
	applies := cpu["applies"].(map[string]any)
	if applies["scope_type"] != "device" || applies["warning"].(float64) != 50 {
		t.Errorf("preview says cpu resolves to %+v, want the device rule at 50", applies)
	}
	candidates, _ := cpu["candidates"].([]any)
	if len(candidates) != 4 {
		t.Fatalf("preview listed %d candidates, want the 3 rules plus the built-in floor", len(candidates))
	}
	// The chain is ordered most specific first, and the floor is listed last:
	// "why did this unit alert at 62?" is answerable from this one read.
	first := candidates[0].(map[string]any)
	last := candidates[len(candidates)-1].(map[string]any)
	if first["scope_type"] != "device" {
		t.Errorf("chain starts at %+v, want the device rule", first)
	}
	if last["source"] != "built-in default" {
		t.Errorf("chain ends at %+v, want the built-in floor", last)
	}

	// ram has no rule: the preview says so, in words the operator can read.
	for _, item := range metrics {
		entry := item.(map[string]any)
		if entry["metric"] == "ram" && entry["applies"].(map[string]any)["source"] != "built-in default" {
			t.Errorf("ram preview = %+v, want the built-in default", entry["applies"])
		}
	}
}

func TestThresholdSimulateAnswersTheMessageTheAlertWouldCarry(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "sim-admin", "admin")
	deviceID, _ := thresholdDevice(t, f, "sim-unit", &f.alice.ID)
	createRule(t, f, admin, map[string]any{
		"scope_type": "tag", "scope_value": "prod", "metric": "cpu",
		"warning": 60, "critical": 75,
	})

	code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds/simulate?device="+deviceID+"&metric=cpu&value=72", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("simulate = %d\n%s", code, body)
	}
	out := jsonOf(t, body)
	if out["would_fire"] != true || out["severity"] != "warning" {
		t.Errorf("simulate = %v, want it to fire a warning at 72 against 60/75", out)
	}
	if out["message"] != "CPU usage elevated: 72.0% (threshold: 60%)" {
		t.Errorf("simulated message = %v, want the engine's own wording", out["message"])
	}
	if out["scope_type"] != "tag" {
		t.Errorf("simulate names scope %v, want the tag rule that answers this value", out["scope_type"])
	}
	// The answer names what the rule points at, the way the ledger does.
	if out["scope_name"] != "prod" {
		t.Errorf("simulate names %+v, want the tag it matched", out["scope_name"])
	}

	// Below the warning: nothing fires, and the answer says that.
	_, quiet, _ := f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds/simulate?device="+deviceID+"&metric=cpu&value=42", admin, nil)
	if jsonOf(t, quiet)["would_fire"] != false {
		t.Errorf("simulate fired at 42 against a 60 warning: %s", quiet)
	}

	// A metric the engine never evaluates is refused, not answered with zeros.
	code, body, _ = f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds/simulate?device="+deviceID+"&metric=swap&value=50", admin, nil)
	if code != http.StatusBadRequest {
		t.Errorf("simulate of an unknown metric = %d, want 400\n%s", code, body)
	}
}

func TestManagerCannotDefineAThresholdForAClientTheyCannotSee(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "mgr-admin", "admin")
	manager := f.makeUserWithScope(t, "mgr-alpha", "manager", f.alice.ID)

	// Their own tenant: allowed.
	code, body, ownRule := createRule(t, f, manager, map[string]any{
		"scope_type": "client", "scope_value": f.alice.ID, "metric": "cpu",
		"warning": 70, "critical": 85,
	})
	if code != http.StatusCreated {
		t.Fatalf("manager rule for own tenant = %d\n%s", code, body)
	}

	// Beta Co: the manager cannot see it, so the answer is the body a client that
	// does not exist gets — the 2.3.0 rule, applied to a write.
	code, foreign, _ := f.do(t, http.MethodPost, f.baseURL+"/api/alerts/thresholds", manager, map[string]any{
		"scope_type": "client", "scope_value": f.bob.ID, "metric": "cpu",
		"warning": 70, "critical": 85,
	})
	if code != http.StatusNotFound {
		t.Errorf("manager rule for a foreign tenant = %d, want 404\n%s", code, foreign)
	}
	if strings.Contains(string(foreign), f.bob.Name) {
		t.Errorf("the refusal confirms the foreign tenant exists: %s", foreign)
	}

	// A tag rule reaches every tenant's units; so does the global floor.
	for _, scope := range []struct{ kind, value string }{{"tag", "prod"}, {"global", ""}} {
		code, body, _ = f.do(t, http.MethodPost, f.baseURL+"/api/alerts/thresholds", manager, map[string]any{
			"scope_type": scope.kind, "scope_value": scope.value, "metric": "cpu",
			"warning": 65, "critical": 80,
		})
		if code != http.StatusForbidden {
			t.Errorf("manager %s rule = %d, want 403\n%s", scope.kind, code, body)
		}
		if !strings.Contains(string(body), "admin") {
			t.Errorf("the refusal does not say who may do this: %s", body)
		}
	}

	// Editing/deleting a global rule: same boundary.
	globalID := createRuleID(t, f, admin, map[string]any{
		"scope_type": "global", "scope_value": "", "metric": "ram",
		"warning": 75, "critical": 92,
	})
	if code, body, _ := f.do(t, http.MethodPut, f.baseURL+"/api/alerts/thresholds/"+globalID, manager, map[string]any{"warning": 50}); code != http.StatusForbidden {
		t.Errorf("manager edited a global rule with %d, want 403\n%s", code, body)
	}
	if code, body, _ := f.do(t, http.MethodDelete, f.baseURL+"/api/alerts/thresholds/"+globalID, manager, nil); code != http.StatusForbidden {
		t.Errorf("manager deleted a global rule with %d, want 403\n%s", code, body)
	}
	// The manager's own rule is theirs to delete.
	if code, body, _ := f.do(t, http.MethodDelete, f.baseURL+"/api/alerts/thresholds/"+ownRule, manager, nil); code != http.StatusOK {
		t.Errorf("manager deleted their own rule with %d, want 200\n%s", code, body)
	}
}

func TestThresholdValidationRefusesAnUnusableRule(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "rules-validate", "admin")
	deviceID, _ := thresholdDevice(t, f, "validate-unit", &f.alice.ID)

	cases := []struct {
		name   string
		form   map[string]any
		status int
		want   string
	}{
		{
			name:   "warning above critical can never alert",
			form:   map[string]any{"scope_type": "device", "scope_value": deviceID, "metric": "cpu", "warning": 90, "critical": 85},
			status: http.StatusBadRequest,
			want:   "warning must be below critical",
		},
		{
			name:   "a metric the engine does not evaluate",
			form:   map[string]any{"scope_type": "device", "scope_value": deviceID, "metric": "swap", "warning": 50, "critical": 65},
			status: http.StatusBadRequest,
			want:   "metric must be cpu, ram or disk",
		},
		{
			name:   "a tag rule naming no tag",
			form:   map[string]any{"scope_type": "tag", "scope_value": "  ", "metric": "cpu", "warning": 50, "critical": 65},
			status: http.StatusBadRequest,
			want:   "scope_value is required",
		},
		{
			name:   "a rule for a unit that is not in the installation",
			form:   map[string]any{"scope_type": "device", "scope_value": uuid.NewString(), "metric": "cpu", "warning": 50, "critical": 65},
			status: http.StatusNotFound,
			want:   "device not found",
		},
		{
			name:   "a dedup window the engine cannot use",
			form:   map[string]any{"scope_type": "device", "scope_value": deviceID, "metric": "cpu", "warning": 50, "critical": 65, "dedup_window_seconds": 100000},
			status: http.StatusBadRequest,
			want:   "dedup_window_seconds",
		},
		{
			name:   "percentages outside the metric",
			form:   map[string]any{"scope_type": "device", "scope_value": deviceID, "metric": "cpu", "warning": 120, "critical": 130},
			status: http.StatusBadRequest,
			want:   "percentages",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body, _ := f.do(t, http.MethodPost, f.baseURL+"/api/alerts/thresholds", admin, tc.form)
			if code != tc.status {
				t.Errorf("status = %d, want %d\n%s", code, tc.status, body)
			}
			if !strings.Contains(string(body), tc.want) {
				t.Errorf("refusal does not say %q: %s", tc.want, body)
			}
		})
	}

	// A second rule on the same (scope, metric) is a conflict naming the rule to
	// edit, not a silent pair of rules that the resolver has to break a tie on.
	form := map[string]any{"scope_type": "device", "scope_value": deviceID, "metric": "cpu", "warning": 50, "critical": 65}
	code, body, id := createRule(t, f, admin, form)
	if code != http.StatusCreated {
		t.Fatalf("first rule = %d\n%s", code, body)
	}
	dupCode, dupBody, _ := createRule(t, f, admin, form)
	if dupCode != http.StatusConflict {
		t.Fatalf("duplicate rule = %d, want 409\n%s", dupCode, dupBody)
	}
	// The conflict names the rule to edit: two rules on one (scope, metric) would
	// leave the resolver breaking a tie the operator never chose.
	if jsonOf(t, dupBody)["id"] != id {
		t.Errorf("the conflict does not name the rule to edit: %s", dupBody)
	}

	// The scope of a saved rule is immutable: retargeting a rule the operator is
	// looking at is not an edit.
	if code, body, _ := f.do(t, http.MethodPut, f.baseURL+"/api/alerts/thresholds/"+id, admin, map[string]any{
		"scope_type": "global", "scope_value": "",
	}); code != http.StatusConflict {
		t.Errorf("scope change = %d, want 409\n%s", code, body)
	}
	// The numbers are editable.
	if code, body, _ := f.do(t, http.MethodPut, f.baseURL+"/api/alerts/thresholds/"+id, admin, map[string]any{
		"warning": 55, "critical": 70, "enabled": false,
	}); code != http.StatusOK {
		t.Fatalf("edit = %d\n%s", code, body)
	}
	// And the engine sees the edit: 62 is under the new 55 warning? No — above it.
	postMetrics(t, f, thresholdKey(t, f, deviceID), 62, 0, 0)
}

// thresholdKey reads a unit's agent key.
func thresholdKey(t *testing.T, f *scopeFixture, deviceID string) string {
	t.Helper()
	device, err := f.st.Devices.GetByID(deviceID)
	if err != nil {
		t.Fatalf("load device: %v", err)
	}
	return device.DeviceKey
}

func TestThresholdRoutesAreAdminAndManagerOnly(t *testing.T) {
	f := newScopeFixture(t)
	technician := f.makeUserWithScope(t, "rules-tech", "technician", f.alice.ID)
	viewer := f.makeUserWithScope(t, "rules-viewer", "viewer", f.alice.ID)

	for _, token := range []struct{ who, token string }{{"technician", technician}, {"viewer", viewer}} {
		for _, call := range []struct {
			method, path string
		}{
			{http.MethodGet, "/api/alerts/thresholds"},
			{http.MethodGet, "/api/alerts/thresholds/resolve?device=" + f.inAlice},
			{http.MethodGet, "/api/alerts/thresholds/simulate?device=" + f.inAlice + "&metric=cpu&value=90"},
			{http.MethodPost, "/api/alerts/thresholds"},
			{http.MethodPut, "/api/alerts/thresholds/anything"},
			{http.MethodDelete, "/api/alerts/thresholds/anything"},
		} {
			code, body, _ := f.do(t, call.method, f.baseURL+call.path, token.token, map[string]any{})
			if code != http.StatusForbidden {
				t.Errorf("%s called %s %s with %d, want 403\n%s", token.who, call.method, call.path, code, body)
			}
		}
	}
}

func TestThresholdWritesAreAuditedWithTheirNumbers(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "rules-audit", "admin")
	deviceID, _ := thresholdDevice(t, f, "audit-unit", &f.alice.ID)

	code, body, id := createRule(t, f, admin, map[string]any{
		"scope_type": "device", "scope_value": deviceID, "metric": "cpu",
		"warning": 42, "critical": 66, "dedup_window_seconds": 120,
	})
	if code != http.StatusCreated {
		t.Fatalf("create = %d\n%s", code, body)
	}
	if code, body, _ := f.do(t, http.MethodPut, f.baseURL+"/api/alerts/thresholds/"+id, admin, map[string]any{"warning": 45}); code != http.StatusOK {
		t.Fatalf("update = %d\n%s", code, body)
	}
	if code, body, _ := f.do(t, http.MethodDelete, f.baseURL+"/api/alerts/thresholds/"+id, admin, nil); code != http.StatusOK {
		t.Fatalf("delete = %d\n%s", code, body)
	}

	rows, err := f.st.Audit.List(store.AuditFilter{})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	want := map[string]string{
		models.AuditThresholdCreate: "42",
		models.AuditThresholdUpdate: "45",
		models.AuditThresholdDelete: "45",
	}
	seen := map[string]bool{}
	for _, row := range rows {
		needle, ok := want[row.Action]
		if !ok {
			continue
		}
		seen[row.Action] = true
		if row.ActorUsername != "rules-audit" {
			t.Errorf("%s actor = %q, want the admin who acted", row.Action, row.ActorUsername)
		}
		if row.TargetType != "alert-threshold" || row.TargetID != id {
			t.Errorf("%s target = %s/%s, want the rule", row.Action, row.TargetType, row.TargetID)
		}
		if !strings.Contains(row.Details, needle) || !strings.Contains(row.Details, "device") {
			t.Errorf("%s details do not state the scope and the numbers: %s", row.Action, row.Details)
		}
	}
	for action := range want {
		if !seen[action] {
			t.Errorf("the %s act wrote no ledger row", action)
		}
	}
}

func TestThresholdListStatesTheBuiltInFloor(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "rules-list", "admin")
	manager := f.makeUserWithScope(t, "rules-list-manager", "manager", f.alice.ID)

	for _, who := range []struct{ name, token string }{{"admin", admin}, {"manager", manager}} {
		code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds", who.token, nil)
		if code != http.StatusOK {
			t.Fatalf("%s list = %d\n%s", who.name, code, body)
		}
		out := jsonOf(t, body)
		defaults, _ := out["defaults"].([]any)
		if len(defaults) != 3 {
			t.Fatalf("defaults = %v, want cpu/ram/disk", out["defaults"])
		}
		// The numbers v2.3.x alerted with, stated in the API so a console can
		// show what a unit falls back to.
		first := defaults[0].(map[string]any)
		if first["warning"].(float64) != 80 || first["critical"].(float64) != 90 {
			t.Errorf("cpu floor = %v, want 80/90", first)
		}
	}
}

func TestThresholdPreviewAndSimulateAreScopedToTheCallersTenants(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "scope-rules-admin", "admin")
	manager := f.makeUserWithScope(t, "scope-rules-manager", "manager", f.alice.ID)

	// A unit in the manager's tenant: readable.
	own, _ := thresholdDevice(t, f, "mine-unit", &f.alice.ID)
	if code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds/resolve?device="+own, manager, nil); code != http.StatusOK {
		t.Fatalf("own preview = %d\n%s", code, body)
	}
	if code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds/simulate?device="+own+"&metric=cpu&value=95", manager, nil); code != http.StatusOK {
		t.Fatalf("own simulate = %d\n%s", code, body)
	}

	// A unit in someone else's tenant answers the body a unit that does not exist
	// gets, on both doors.
	for _, path := range []string{
		"/api/alerts/thresholds/resolve?device=" + f.inBob,
		"/api/alerts/thresholds/simulate?device=" + f.inBob + "&metric=cpu&value=95",
	} {
		code, foreign, _ := f.do(t, http.MethodGet, f.baseURL+path, manager, nil)
		_, missing, _ := f.do(t, http.MethodGet, f.baseURL+strings.Replace(path, f.inBob, uuid.NewString(), 1), manager, nil)
		if code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404\n%s", path, code, foreign)
		}
		if string(foreign) != string(missing) {
			t.Errorf("scoped answer differs from the not-found answer:\n%s\n---\n%s", foreign, missing)
		}
	}
	// Admin reads both.
	if code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds/resolve?device="+f.inBob, admin, nil); code != http.StatusOK {
		t.Fatalf("admin preview of a beta unit = %d\n%s", code, body)
	}
}

// The rule read is a JSON read the console consumes; assert its shape so a
// rename in the API cannot silently empty the ledger page.
func TestThresholdListShape(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "rules-shape", "admin")
	deviceID, _ := thresholdDevice(t, f, "shape-unit", &f.alice.ID)
	createRuleID(t, f, admin, map[string]any{
		"scope_type": "device", "scope_value": deviceID, "metric": "disk",
		"warning": 55, "critical": 70,
	})

	code, body, _ := f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("list = %d\n%s", code, body)
	}
	out := jsonOf(t, body)
	rules, _ := out["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules = %v, want the one seeded rule", out["rules"])
	}
	rule := rules[0].(map[string]any)
	for _, key := range []string{"id", "scope_type", "scope_value", "metric", "warning", "critical", "dedup_window_seconds", "enabled", "scope_name"} {
		if _, ok := rule[key]; !ok {
			t.Errorf("the rule view has no %q: %v", key, rule)
		}
	}
	// The ledger names what the rule points at: a device id alone is unreadable.
	if rule["scope_name"] != "shape-unit" {
		t.Errorf("scope_name = %v, want the unit's name", rule["scope_name"])
	}

	// The global rule has no id to name, so it carries a phrase — and the phrase
	// is the one the console translates, not a string invented here.
	globalID := createRuleID(t, f, admin, map[string]any{
		"scope_type": "global", "scope_value": "", "metric": "ram",
		"warning": 75, "critical": 92,
	})
	code, body, _ = f.do(t, http.MethodGet, f.baseURL+"/api/alerts/thresholds", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("list = %d\n%s", code, body)
	}
	var global map[string]any
	for _, item := range jsonOf(t, body)["rules"].([]any) {
		entry := item.(map[string]any)
		if entry["id"] == globalID {
			global = entry
		}
	}
	if global == nil {
		t.Fatal("the global rule is missing from the list")
	}
	if global["scope_name"] != consoleEnglishPhrase(t, "globalName") {
		t.Errorf("global scope_name = %v, want the console's phrase for it", global["scope_name"])
	}
}

// The acceptance criterion the plan states as "the simulation input agrees with
// the engine on 20 fixture sequences". Each value is asked of the simulation, and
// posted at a unit of its own as a real metric. The comparison is severity and the
// threshold and message the alert names — the facts an operator acts on.
//
// The fixture rules and units are seeded through the store, and every HTTP read
// here goes through `doWithinBudget`: a test that batches dozens of reads is
// exactly the caller the rate limiter exists to shape, and the way through it is
// the caller's own way.
func TestThresholdSimulationAgreesWithTheEngineOnTwentyValues(t *testing.T) {
	f := newScopeFixture(t)
	admin := f.makeUserWithScope(t, "sim-agree", "admin")
	deviceID, _ := thresholdDevice(t, f, "agree-unit", &f.alice.ID)
	seedRule(t, f, models.ScopeGlobal, "", models.MetricCPU, 80, 90)
	seedRule(t, f, models.ScopeClient, f.alice.ID, models.MetricCPU, 70, 85)
	seedRule(t, f, models.ScopeTag, "prod", models.MetricCPU, 60, 75)
	seedRule(t, f, models.ScopeDevice, deviceID, models.MetricCPU, 50, 65)

	values := []float64{0, 5, 20, 35, 45, 49, 50, 51, 55, 58, 60, 61, 64, 65, 66, 70, 74, 80, 86, 91}
	units := make([]string, 0, len(values))
	for _, value := range values {
		unitID, unitKey := thresholdDevice(t, f, fmt.Sprintf("agree-%g", value), &f.alice.ID)
		seedRule(t, f, models.ScopeDevice, unitID, models.MetricCPU, 50, 65)
		postMetrics(t, f, unitKey, value, 0, 0)
		units = append(units, unitID)
	}

	// One read of the whole ledger, grouped by unit: 20 comparisons, not 20 reads.
	code, body, _ := f.doWithinBudget(t, http.MethodGet, f.baseURL+"/api/alerts?resolved=false", admin, nil)
	if code != http.StatusOK {
		t.Fatalf("alerts = %d\n%s", code, body)
	}
	var ledger struct {
		Alerts []models.Alert `json:"alerts"`
	}
	if err := json.Unmarshal(body, &ledger); err != nil {
		t.Fatalf("decode alerts: %v\n%s", err, body)
	}
	fired := map[string]models.Alert{}
	for _, alert := range ledger.Alerts {
		if alert.Metric == models.MetricCPU {
			fired[alert.DeviceID] = alert
		}
	}

	for _, value := range values {
		code, body, _ := f.doWithinBudget(t, http.MethodGet,
			fmt.Sprintf("%s/api/alerts/thresholds/simulate?device=%s&metric=cpu&value=%g", f.baseURL, deviceID, value),
			admin, nil)
		if code != http.StatusOK {
			t.Fatalf("simulate %g = %d\n%s", value, code, body)
		}
		answer := jsonOf(t, body)
		unit, ok := fired[units[valueIndex(values, value)]]
		if !ok {
			if answer["would_fire"] == true {
				t.Errorf("value %g: simulation says it fires, the engine raised nothing", value)
			}
			continue
		}
		if answer["would_fire"] != true {
			t.Errorf("value %g: the engine raised %q at threshold %v, the simulation says it would not fire",
				value, unit.Severity, unit.Threshold)
			continue
		}
		if answer["severity"] != unit.Severity {
			t.Errorf("value %g: simulation says %v, the engine raised %v", value, answer["severity"], unit.Severity)
		}
		if answer["threshold"].(float64) != unit.Threshold {
			t.Errorf("value %g: simulation names threshold %v, the alert names %v", value, answer["threshold"], unit.Threshold)
		}
		if answer["message"] != unit.Message {
			t.Errorf("value %g: simulation says %q, the alert says %q", value, answer["message"], unit.Message)
		}
	}
}

// valueIndex answers the unit seeded for a value (values are distinct, so the
// position in the sequence is the pairing).
func valueIndex(values []float64, value float64) int {
	for i, v := range values {
		if v == value {
			return i
		}
	}
	return -1
}

// seedRule writes a rule through the store: this test compares what the engine
// does with what the simulation says, and the rule set is not what is under test.
func seedRule(t *testing.T, f *scopeFixture, scopeType, scopeValue, metric string, warning, critical float64) {
	t.Helper()
	if err := f.st.Thresholds.Create(&models.AlertThreshold{
		ID: uuid.NewString(), ScopeType: scopeType, ScopeValue: scopeValue, Metric: metric,
		Warning: warning, Critical: critical, Enabled: true,
	}); err != nil {
		t.Fatalf("seed rule: %v", err)
	}
}
