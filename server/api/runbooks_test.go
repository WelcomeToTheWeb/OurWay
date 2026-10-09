package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

func newRunbookTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory SQLite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	jwtAuth := auth.NewJWTAuth("test-secret-key")
	hub := ws.NewHub("http://localhost:3000", "")
	go hub.Run()
	engine := alerts.NewEngine(st.Alerts)
	router := SetupRouter(context.Background(), st, jwtAuth, hub, engine, "http://localhost:3000", "")
	ts := httptest.NewServer(router)
	t.Cleanup(func() {
		ts.Close()
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return ts, st
}

// registerAdmin creates the first user (admin) and returns its access token.
func registerAdmin(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	regBody, _ := json.Marshal(map[string]string{
		"username": "admin", "email": "admin@example.com", "password": "password123",
	})
	resp, err := http.Post(ts.URL+"/api/auth/register", "application/json", bytes.NewReader(regBody))
	if err != nil || resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("register: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()
	loginBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "password123"})
	resp, err = http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("login: %v %d", err, resp.StatusCode)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		t.Fatalf("decode login: %v", err)
	}
	return out.AccessToken
}

func TestRunbookCRUD(t *testing.T) {
	ts, _ := newRunbookTestServer(t)
	token := registerAdmin(t, ts)

	create := map[string]interface{}{
		"name": "disk cleanup", "scope": "tags", "scope_value": "prod",
		"schedule": "daily", "command": "df -h", "enabled": true,
	}
	body, _ := json.Marshal(create)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v2/automation/runbooks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("create runbook: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// List and find it.
	req, _ = http.NewRequest("GET", ts.URL+"/api/v2/automation/runbooks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("list runbooks: %v %d", err, resp.StatusCode)
	}
	var list struct {
		Runbooks []models.Runbook `json:"runbooks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	resp.Body.Close()
	if len(list.Runbooks) != 1 {
		t.Fatalf("expected 1 runbook, got %d", len(list.Runbooks))
	}
	id := list.Runbooks[0].ID

	// Invalid scope must be rejected.
	bad, _ := json.Marshal(map[string]interface{}{"name": "x", "scope": "bogus", "schedule": "daily", "command": "true"})
	req, _ = http.NewRequest("POST", ts.URL+"/api/v2/automation/runbooks", bytes.NewReader(bad))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 400 {
		t.Errorf("invalid scope should be rejected: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Update it (disable).
	upd, _ := json.Marshal(map[string]interface{}{
		"name": "disk cleanup", "scope": "all", "schedule": "weekly", "command": "df -h", "enabled": false,
	})
	req, _ = http.NewRequest("PUT", ts.URL+"/api/v2/automation/runbooks/"+id, bytes.NewReader(upd))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Errorf("update runbook: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Delete it.
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/v2/automation/runbooks/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Errorf("delete runbook: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Deleting again is 404.
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/v2/automation/runbooks/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 404 {
		t.Errorf("delete unknown runbook should be 404: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()
}

func TestReportRunResultRejectsWrongDevice(t *testing.T) {
	ts, st := newRunbookTestServer(t)
	_ = registerAdmin(t, ts)

	dev := &models.Device{ID: "d1", Name: "laptop", Hostname: "laptop", OS: "linux", Arch: "amd64", DeviceKey: "k1", Status: "online"}
	if err := st.Devices.Create(dev); err != nil {
		t.Fatalf("create device: %v", err)
	}
	run := &models.RunbookRun{ID: "run1", RunbookID: "rb1", DeviceID: "d1", Status: "pending"}
	if err := st.Runbooks.CreateRun(run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Wrong key: 401.
	body, _ := json.Marshal(map[string]interface{}{"run_id": "run1", "device_id": "d1", "success": true})
	resp, err := http.Post(ts.URL+"/api/agent/runbooks/result", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 401 {
		t.Errorf("missing key should be 401: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Right key, wrong device: 403.
	wrongDev, _ := json.Marshal(map[string]interface{}{"run_id": "run1", "device_id": "d2", "success": true})
	req, _ := http.NewRequest("POST", ts.URL+"/api/agent/runbooks/result", bytes.NewReader(wrongDev))
	req.Header.Set("X-Device-Key", "k1")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 403 {
		t.Errorf("mismatched device should be 403: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Wrong run id: 404.
	body2, _ := json.Marshal(map[string]interface{}{"run_id": "nope", "device_id": "d1", "success": true})
	req, _ = http.NewRequest("POST", ts.URL+"/api/agent/runbooks/result", bytes.NewReader(body2))
	req.Header.Set("X-Device-Key", "k1")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 404 {
		t.Errorf("unknown run should be 404: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Happy path with the correct device key.
	body3, _ := json.Marshal(map[string]interface{}{
		"run_id": "run1", "device_id": "d1", "success": true, "exit_code": 0, "output": "all good",
	})
	req, _ = http.NewRequest("POST", ts.URL+"/api/agent/runbooks/result", bytes.NewReader(body3))
	req.Header.Set("X-Device-Key", "k1")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Errorf("valid report should be 200: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	got, err := st.Runbooks.GetRun("run1")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != "success" || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("run not recorded correctly: %+v", got)
	}
	if got.FinishedAt == nil {
		t.Error("finished run should carry FinishedAt")
	}
}
