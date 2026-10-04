package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/models"
	"ourway/server/patching"
	"ourway/server/store"
	"ourway/server/ws"
)

func fleetFixture(t *testing.T) (*gin.Engine, *store.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub("http://localhost:3000", "")
	h := NewPatchHandler(st, patching.NewScanner(st, hub), patching.NewDeployer(st, hub), patching.NewRollbackManager(st, hub))
	r := gin.New()
	r.GET("/overview", h.PatchOverview)
	r.GET("/updates", h.ListFleetUpdates)
	r.POST("/approve", h.BulkApprove)
	r.POST("/skip", h.BulkSkip)

	for _, d := range []*models.Device{
		{ID: "d1", DeviceKey: "k1", Name: "alpha", Hostname: "a", OS: "windows", Arch: "amd64", Status: "online"},
		{ID: "d2", DeviceKey: "k2", Name: "beta", Hostname: "b", OS: "windows", Arch: "amd64", Status: "online"},
		{ID: "d3", DeviceKey: "k3", Name: "gamma", Hostname: "c", OS: "linux", Arch: "amd64", Status: "online"},
	} {
		if err := st.Devices.Create(d); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(id, dev, status, sev string) {
		if err := st.SoftwareUpdates.Create(&models.SoftwareUpdate{ID: id, DeviceID: dev, Source: "windows-update",
			Title: "KB5001", ExternalID: "ext-5001", Severity: sev, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	mk("u1", "d1", "detected", "critical")
	mk("u2", "d2", "detected", "critical")
	_ = st.SoftwareUpdates.Create(&models.SoftwareUpdate{ID: "u3", DeviceID: "d2", Source: "apt", Title: "curl", Status: "installed"})
	return r, st
}

func doJSON(r *gin.Engine, method, path string, body interface{}) (int, map[string]interface{}) {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestPatchOverviewCompliance(t *testing.T) {
	r, _ := fleetFixture(t)
	code, out := doJSON(r, "GET", "/overview", nil)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	tot := out["totals"].(map[string]interface{})
	if tot["devices"].(float64) != 3 || tot["compliant"].(float64) != 1 || tot["pending"].(float64) != 2 || tot["critical"].(float64) != 2 {
		t.Fatalf("unexpected totals: %v", tot)
	}
	// Installed updates do not count against compliance; d3 (none) is compliant.
	first := out["devices"].([]interface{})[0].(map[string]interface{})
	if first["compliant"].(bool) {
		t.Fatal("worst-first ordering: first row must be non-compliant")
	}
}

func TestFleetUpdatesGroupAndBulkApprove(t *testing.T) {
	r, st := fleetFixture(t)
	_, out := doJSON(r, "GET", "/updates", nil)
	groups := out["updates"].([]interface{})
	if len(groups) != 1 {
		t.Fatalf("same update on two devices must be one group, got %d", len(groups))
	}
	g := groups[0].(map[string]interface{})
	if g["devices"].(float64) != 2 || g["severity"] != "critical" {
		t.Fatalf("bad group: %v", g)
	}
	ids := g["detected_ids"].([]interface{})
	code, res := doJSON(r, "POST", "/approve", map[string]interface{}{"update_ids": ids})
	if code != 200 || res["changed"].(float64) != 2 {
		t.Fatalf("approve: %d %v", code, res)
	}
	if u, _ := st.SoftwareUpdates.GetByID("u1"); u.Status != "approved" {
		t.Fatalf("u1 status %q", u.Status)
	}
	// Approving again is a no-op (only detected updates change).
	_, res = doJSON(r, "POST", "/approve", map[string]interface{}{"update_ids": ids})
	if res["changed"].(float64) != 0 {
		t.Fatalf("second approve changed rows: %v", res)
	}
	if code, _ := doJSON(r, "POST", "/skip", map[string]interface{}{"update_ids": []string{}}); code != http.StatusBadRequest {
		t.Fatalf("empty ids must 400, got %d", code)
	}
}
