package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// newTestServer sets up an httptest server with a fresh in-memory SQLite database,
// the Gin router, and returns the test server plus the store for assertions.
func newTestServer(t *testing.T) (*httptest.Server, *store.Store, *auth.JWTAuth) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory SQLite: %v", err)
	}

	st, err := store.NewWithDB(db)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	jwtAuth := auth.NewJWTAuth("test-secret-key")
	hub := ws.NewHub()
	engine := alerts.NewEngine(st.Alerts)

	router := SetupRouter(st, jwtAuth, hub, engine)
	ts := httptest.NewServer(router)

	t.Cleanup(func() {
		ts.Close()
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})

	return ts, st, jwtAuth
}

// registerUser registers a test user and returns the response.
func registerUser(t *testing.T, baseURL string, username, email, password string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	})
	resp, err := http.Post(baseURL+"/api/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	return resp
}

// loginUser logs in and returns the access token.
func loginUser(t *testing.T, baseURL string, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	resp, err := http.Post(baseURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("login failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}

	token, ok := result["access_token"].(string)
	if !ok || token == "" {
		t.Fatal("no access_token in login response")
	}
	return token
}

func TestHealthEndpoint(t *testing.T) {
	ts, _, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", body["status"])
	}
}

func TestUserRegistration(t *testing.T) {
	ts, _, _ := newTestServer(t)

	t.Run("successful registration", func(t *testing.T) {
		resp := registerUser(t, ts.URL, "testuser", "test@example.com", "password123")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 201, got %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if user, ok := result["user"].(map[string]interface{}); !ok || user["username"] != "testuser" {
			t.Error("expected user with username 'testuser' in response")
		}
	})

	t.Run("duplicate username registration", func(t *testing.T) {
		registerUser(t, ts.URL, "dupuser", "dup1@example.com", "pass1")

		resp := registerUser(t, ts.URL, "dupuser", "dup2@example.com", "pass2")
		defer resp.Body.Close()

		// SQLite returns a constraint violation -> 500 from handler
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected 500 for duplicate, got %d", resp.StatusCode)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"username": "nouser"})
		resp, err := http.Post(ts.URL+"/api/auth/register", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for missing fields, got %d", resp.StatusCode)
		}
	})
}

func TestUserLogin(t *testing.T) {
	ts, _, _ := newTestServer(t)

	t.Run("successful login", func(t *testing.T) {
		registerUser(t, ts.URL, "loginuser", "login@example.com", "secretpass")

		token := loginUser(t, ts.URL, "loginuser", "secretpass")
		if token == "" {
			t.Error("expected non-empty token")
		}

		// Verify token can be validated
		claims, err := auth.NewJWTAuth("test-secret-key").ValidateToken(token)
		if err != nil {
			t.Errorf("token validation failed: %v", err)
		}
		if claims.Username != "loginuser" {
			t.Errorf("expected username 'loginuser' in claims, got %q", claims.Username)
		}
	})

	t.Run("login with wrong password", func(t *testing.T) {
		registerUser(t, ts.URL, "wrongpassuser", "wrongpass@example.com", "correctpass")

		body, _ := json.Marshal(map[string]string{
			"username": "wrongpassuser",
			"password": "wrongpassword",
		})
		resp, err := http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 for wrong password, got %d", resp.StatusCode)
		}
	})

	t.Run("login with non-existent user", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"username": "ghostuser",
			"password": "anypass",
		})
		resp, err := http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 for non-existent user, got %d", resp.StatusCode)
		}
	})
}

func TestDeviceRegistration(t *testing.T) {
	ts, _, _ := newTestServer(t)

	t.Run("successful device registration", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"name":          "test-device",
			"hostname":      "test-host",
			"os":            "linux",
			"arch":          "amd64",
			"agent_version": "1.0.0",
		})
		resp, err := http.Post(ts.URL+"/api/agent/register", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("register request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 201, got %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		device, ok := result["device"].(map[string]interface{})
		if !ok {
			t.Fatal("no device in response")
		}
		if device["name"] != "test-device" {
			t.Errorf("expected device name 'test-device', got %v", device["name"])
		}
		if device["status"] != "online" {
			t.Errorf("expected device status 'online', got %v", device["status"])
		}

		deviceKey, ok := result["device_key"].(string)
		if !ok || deviceKey == "" {
			t.Error("expected non-empty device_key")
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "incomplete"})
		resp, err := http.Post(ts.URL+"/api/agent/register", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for missing fields, got %d", resp.StatusCode)
		}
	})
}

func TestHeartbeat(t *testing.T) {
	ts, st, _ := newTestServer(t)

	// Register a device first
	device := &models.Device{
		ID:        uuid.New().String(),
		Name:      "heartbeat-device",
		Hostname:  "hb-host",
		OS:        "linux",
		Arch:      "amd64",
		Status:    "online",
		DeviceKey: "test-device-key",
	}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	// Record last seen before heartbeat
	oldDevice, _ := st.Devices.GetByID(device.ID)
	time.Sleep(10 * time.Millisecond)

	t.Run("successful heartbeat", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/agent/heartbeat", nil)
		req.Header.Set("X-Device-Key", "test-device-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("heartbeat request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}

		// Verify last_seen was updated
		newDevice, err := st.Devices.GetByID(device.ID)
		if err != nil {
			t.Fatalf("failed to get device after heartbeat: %v", err)
		}
		if !newDevice.LastSeen.After(oldDevice.LastSeen) {
			t.Error("last_seen was not updated after heartbeat")
		}
	})

	t.Run("heartbeat with missing key", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/agent/heartbeat", "application/json", nil)
		if err != nil {
			t.Fatalf("heartbeat request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 for missing key, got %d", resp.StatusCode)
		}
	})

	t.Run("heartbeat with invalid key", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/agent/heartbeat", nil)
		req.Header.Set("X-Device-Key", "nonexistent-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("heartbeat request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 for invalid key, got %d", resp.StatusCode)
		}
	})
}

func TestMetricsEndpoint(t *testing.T) {
	ts, st, _ := newTestServer(t)

	// Register a device first
	device := &models.Device{
		ID:        uuid.New().String(),
		Name:      "metrics-device",
		Hostname:  "metrics-host",
		OS:        "linux",
		Arch:      "amd64",
		Status:    "online",
		DeviceKey: "metrics-device-key",
	}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	metrics := models.Metrics{
		CPU: 75.5, RAM: 60.2, DiskUsage: 45.0,
		RAMUsed: 6 * 1024 * 1024 * 1024, RAMTotal: 10 * 1024 * 1024 * 1024,
		Processes: 150, Uptime: 86400,
	}

	t.Run("successful metrics report", func(t *testing.T) {
		body, _ := json.Marshal(metrics)
		req, _ := http.NewRequest("POST", ts.URL+"/api/agent/metrics", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Key", "metrics-device-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("metrics request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
	})

	t.Run("high CPU generates alert", func(t *testing.T) {
		highCPUMetrics := metrics
		highCPUMetrics.CPU = 95.0

		body, _ := json.Marshal(highCPUMetrics)
		req, _ := http.NewRequest("POST", ts.URL+"/api/agent/metrics", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Key", "metrics-device-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("metrics request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		// Check that an alert was created
		alerts, err := st.Alerts.List(nil, false)
		if err != nil {
			t.Fatalf("failed to list alerts: %v", err)
		}
		if len(alerts) == 0 {
			t.Error("expected an alert to be created for high CPU")
		} else {
			found := false
			for _, a := range alerts {
				if a.Metric == "cpu" && a.Severity == "critical" {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected a critical CPU alert")
			}
		}
	})
}

func TestJWTAuthMiddleware(t *testing.T) {
	ts, _, jwtAuth := newTestServer(t)

	t.Run("missing authorization header", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/devices")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/devices", nil)
		req.Header.Set("Authorization", "Bearer invalid-token-here")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 for invalid token, got %d", resp.StatusCode)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})
		req, _ := http.NewRequest("GET", ts.URL+"/api/devices", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200 for valid token, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
	})

	t.Run("token without Bearer prefix still works", func(t *testing.T) {
		token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})
		req, _ := http.NewRequest("GET", ts.URL+"/api/devices", nil)
		req.Header.Set("Authorization", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
	})
}

func TestDeviceListing(t *testing.T) {
	ts, st, jwtAuth := newTestServer(t)

	// Create some devices
	for i := 0; i < 3; i++ {
		device := &models.Device{
			ID:        uuid.New().String(),
			Name:      fmt.Sprintf("device-%d", i),
			Hostname:  fmt.Sprintf("host-%d", i),
			OS:        "linux",
			Arch:      "amd64",
			Status:    "online",
			DeviceKey: fmt.Sprintf("key-%d", i),
		}
		if err := st.Devices.Create(device); err != nil {
			t.Fatalf("failed to create device: %v", err)
		}
	}

	token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})
	req, _ := http.NewRequest("GET", ts.URL+"/api/devices", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list devices request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	devices, ok := result["devices"].([]interface{})
	if !ok {
		t.Fatal("expected devices array in response")
	}
	if len(devices) != 3 {
		t.Errorf("expected 3 devices, got %d", len(devices))
	}
}

func TestGetDevice(t *testing.T) {
	ts, st, jwtAuth := newTestServer(t)

	device := &models.Device{
		ID:        uuid.New().String(),
		Name:      "specific-device",
		Hostname:  "specific-host",
		OS:        "linux",
		Arch:      "amd64",
		Status:    "online",
		DeviceKey: "specific-key",
	}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	t.Run("existing device", func(t *testing.T) {
		token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})
		req, _ := http.NewRequest("GET", ts.URL+"/api/devices/"+device.ID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get device request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
	})

	t.Run("non-existent device", func(t *testing.T) {
		token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})
		req, _ := http.NewRequest("GET", ts.URL+"/api/devices/nonexistent-id", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get device request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestDeleteDevice(t *testing.T) {
	ts, st, jwtAuth := newTestServer(t)

	device := &models.Device{
		ID:        uuid.New().String(),
		Name:      "deletable-device",
		Hostname:  "del-host",
		OS:        "linux",
		Arch:      "amd64",
		Status:    "online",
		DeviceKey: "del-key",
	}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/devices/"+device.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete device request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verify device is deleted
	_, err = st.Devices.GetByID(device.ID)
	if err == nil {
		t.Error("expected device to be deleted")
	}
}

func TestAlerts(t *testing.T) {
	ts, st, jwtAuth := newTestServer(t)

	device := &models.Device{
		ID:        uuid.New().String(),
		Name:      "alert-device",
		Hostname:  "alert-host",
		OS:        "linux",
		Arch:      "amd64",
		Status:    "online",
		DeviceKey: "alert-key",
	}
	if err := st.Devices.Create(device); err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	t.Run("list alerts returns empty initially", func(t *testing.T) {
		token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})
		req, _ := http.NewRequest("GET", ts.URL+"/api/alerts", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list alerts request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("alerts are created and can be listed", func(t *testing.T) {
		// Send metrics that trigger an alert
		metrics := models.Metrics{CPU: 99.0, RAM: 50.0, DiskUsage: 30.0}
		body, _ := json.Marshal(metrics)
		req, _ := http.NewRequest("POST", ts.URL+"/api/agent/metrics", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Key", "alert-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("metrics request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 from metrics endpoint, got %d", resp.StatusCode)
		}

		// List alerts
		token, _ := jwtAuth.GenerateToken("test-user-id", "testuser", []string{"admin"})
		req, _ = http.NewRequest("GET", ts.URL+"/api/alerts", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("list alerts request failed: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode alerts response: %v", err)
		}

		alerts, ok := result["alerts"].([]interface{})
		if !ok || len(alerts) == 0 {
			t.Error("expected alerts to be listed")
		}
	})
}

func TestIntegrationFlow(t *testing.T) {
	ts, _, _ := newTestServer(t)

	t.Run("complete user and device flow", func(t *testing.T) {
		// 1. Register user
		resp := registerUser(t, ts.URL, "intuser", "int@example.com", "intpass")
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("user registration failed")
		}

		// 2. Login
		token := loginUser(t, ts.URL, "intuser", "intpass")

		// 3. Register device
		deviceBody, _ := json.Marshal(map[string]string{
			"name": "int-device", "hostname": "int-host", "os": "linux", "arch": "amd64",
		})
		resp, err := http.Post(ts.URL+"/api/agent/register", "application/json", bytes.NewReader(deviceBody))
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("device registration failed")
		}
		defer resp.Body.Close()

		var regResult map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&regResult)
		deviceKey := regResult["device_key"].(string)

		// 4. Heartbeat
		req, _ := http.NewRequest("POST", ts.URL+"/api/agent/heartbeat", nil)
		req.Header.Set("X-Device-Key", deviceKey)
		resp, err = http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("heartbeat failed")
		}
		resp.Body.Close()

		// 5. Report metrics
		metricsBody, _ := json.Marshal(models.Metrics{CPU: 55.0, RAM: 60.0, DiskUsage: 40.0})
		req, _ = http.NewRequest("POST", ts.URL+"/api/agent/metrics", bytes.NewReader(metricsBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Key", deviceKey)
		resp, err = http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("metrics report failed")
		}
		resp.Body.Close()

		// 6. List devices with auth
		req, _ = http.NewRequest("GET", ts.URL+"/api/devices", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err = http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("list devices failed")
		}
		defer resp.Body.Close()

		var listResult map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&listResult)
		devices := listResult["devices"].([]interface{})
		if len(devices) == 0 {
			t.Error("expected at least one device in list")
		}

		// Verify the device is online
		for _, d := range devices {
			dm := d.(map[string]interface{})
			if dm["name"] == "int-device" && dm["status"] == "online" {
				return
			}
		}
		t.Error("device not found as online")
	})
}
