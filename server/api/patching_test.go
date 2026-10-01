package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/patching"
	"ourway/server/store"
	"ourway/server/ws"
)

func setupPatchTest(t *testing.T) (*httptest.Server, *store.Store, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	// In-memory SQLite is per-connection: pin the pool to a single connection
	// so migrations and queries share the same database.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}

	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	jwtAuth := auth.NewJWTAuth("test-secret")
	hub := ws.NewHub()
	scanner := patching.NewScanner(st, hub)
	deployer := patching.NewDeployer(st, hub)
	rollbacker := patching.NewRollbackManager(st, hub)

	router := gin.Default()
	patchHandler := NewPatchHandler(st, scanner, deployer, rollbacker)

	// Register routes
	router.POST("/api/devices/:id/updates/scan", patchHandler.ScanDevice)
	router.GET("/api/devices/:id/updates", patchHandler.ListUpdates)
	router.POST("/api/patch/policies", patchHandler.CreatePolicy)
	router.GET("/api/patch/policies", patchHandler.ListPolicies)
	router.POST("/api/patch/deploy", patchHandler.DeployNow)
	router.GET("/api/patch/deployments", patchHandler.ListDeployments)
	router.POST("/api/patch/deployments/:id/rollback", patchHandler.RollbackDeployment)

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	// Create test user
	user := &models.User{
		ID:           "user-1",
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "hashed",
	}
	st.Users.Create(user)

	// Create test device
	device := &models.Device{
		ID:        "device-1",
		DeviceKey: "test-device-key",
		Name:      "test-device",
		Hostname:  "test-host",
		OS:        "linux",
		Arch:      "amd64",
		Status:    "online",
		LastSeen:  time.Now(),
	}
	st.Devices.Create(device)

	// Generate JWT
	token, err := jwtAuth.GenerateToken("user-1", "testuser", []string{"admin"})
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	return ts, st, token
}

func TestCreatePatchPolicy(t *testing.T) {
	ts, _, token := setupPatchTest(t)

	body, _ := json.Marshal(map[string]interface{}{
		"name":              "Test Policy",
		"scope":             "all",
		"schedule":          "weekly",
		"auto_reboot":       true,
		"approval_required": false,
	})

	req, _ := http.NewRequest("POST", ts.URL+"/api/patch/policies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["policy"] == nil {
		t.Error("expected policy in response")
	}
}

func TestListPatchPolicies(t *testing.T) {
	ts, _, token := setupPatchTest(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/patch/policies", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["policies"] == nil {
		t.Error("expected policies in response")
	}
}

func TestScanDevice(t *testing.T) {
	ts, _, token := setupPatchTest(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/devices/device-1/updates/scan", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "scan_started" {
		t.Errorf("expected status scan_started, got %v", result["status"])
	}
}

func TestScanDeviceNotFound(t *testing.T) {
	ts, _, token := setupPatchTest(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/devices/nonexistent/updates/scan", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestListUpdates(t *testing.T) {
	ts, st, token := setupPatchTest(t)

	// Create a test update
	update := &models.SoftwareUpdate{
		ID:       "update-1",
		DeviceID: "device-1",
		Source:   "apt",
		Title:    "test-package",
		Version:  "1.0.0",
		Status:   "available",
	}
	st.SoftwareUpdates.Create(update)

	req, _ := http.NewRequest("GET", ts.URL+"/api/devices/device-1/updates", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	updates := result["updates"].([]interface{})
	if len(updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(updates))
	}
}

func TestRollbackDeployment(t *testing.T) {
	ts, st, token := setupPatchTest(t)
	now := time.Now()

	// Create a deployment first
	deployment := &models.PatchDeployment{
		ID:             "deploy-1",
		PolicyID:       "policy-1",
		Status:         "completed",
		DevicesTotal:   1,
		DevicesSuccess: 1,
		DevicesFailed:  0,
		StartedAt:      &now,
		CompletedAt:    &now,
		CreatedAt:      now,
	}
	st.PatchDeployments.Create(deployment)

	req, _ := http.NewRequest("POST", ts.URL+"/api/patch/deployments/deploy-1/rollback", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if result["status"] != "rollback_initiated" {
		t.Errorf("expected status rollback_initiated, got %v", result["status"])
	}
}

func TestRollbackDeploymentNotFound(t *testing.T) {
	ts, _, token := setupPatchTest(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/patch/deployments/nonexistent/rollback", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}
