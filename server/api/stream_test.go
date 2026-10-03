package api

// Tests for the device stream endpoints (H5) and their WS round-trip.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"nhooyr.io/websocket"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/store"
	"ourway/server/ws"
)

func newTestServerWithHub(t *testing.T) (*httptest.Server, *store.Store, *ws.Hub) {
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
	// main.go wires the WS upgrade outside SetupRouter; mirror it here.
	router.GET("/ws", func(c *gin.Context) { hub.ServeHTTP(c, st, jwtAuth) })
	ts := httptest.NewServer(router)

	t.Cleanup(func() {
		ts.Close()
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})

	return ts, st, hub
}

func TestStreamEndpointsSmoke(t *testing.T) {
	ts, _, hub := newTestServerWithHub(t)

	// User token
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
	var login struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	resp.Body.Close()
	token := login.AccessToken

	// Device registration
	regDev, _ := json.Marshal(map[string]string{
		"name": "smoke-host", "hostname": "smoke-host", "os": "linux", "arch": "amd64",
		"public_ip": "1.2.3.4", "private_ip": "10.0.0.5",
	})
	resp, err = http.Post(ts.URL+"/api/agent/register", "application/json", bytes.NewReader(regDev))
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("device register: %v %d", err, resp.StatusCode)
	}
	var reg struct {
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
		DeviceKey string `json:"device_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		t.Fatalf("decode device register: %v", err)
	}
	resp.Body.Close()
	deviceID := reg.Device.ID
	deviceKey := reg.DeviceKey

	// Open an agent-style WS connection authenticated via subprotocol.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{
		Subprotocols: []string{"ourway-auth", deviceKey},
	})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.CloseNow()

	post := func(path string, body string, want int) map[string]interface{} {
		t.Helper()
		var req *http.Request
		var err error
		if body == "" {
			req, err = http.NewRequest("POST", ts.URL+path, nil)
		} else {
			req, err = http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
		}
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		defer r.Body.Close()
		if r.StatusCode != want {
			var b bytes.Buffer
			b.ReadFrom(r.Body)
			t.Fatalf("%s: got %d, want %d: %s", path, r.StatusCode, want, b.String())
		}
		var out map[string]interface{}
		json.NewDecoder(r.Body).Decode(&out)
		return out
	}

	readMsg := func() map[string]interface{} {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("ws decode %q: %v", data, err)
		}
		return m
	}

	// 1. Start streaming on a connected device.
	out := post("/api/devices/"+deviceID+"/stream", `{"interval": 2}`, 200)
	if out["status"] != "streaming" {
		t.Fatalf("start: unexpected status %v", out["status"])
	}
	if iv, _ := out["interval"].(float64); iv != 2 {
		t.Fatalf("start: unexpected interval %v", out["interval"])
	}
	m := readMsg()
	if m["type"] != "stream" {
		t.Fatalf("agent got wrong message type: %v", m["type"])
	}
	pl, _ := m["payload"].(map[string]interface{})
	if iv, _ := pl["interval"].(float64); iv != 2 {
		t.Fatalf("agent payload interval wrong: %v", pl)
	}

	// 2. Stop streaming.
	out = post("/api/devices/"+deviceID+"/stream/stop", "", 200)
	if out["status"] != "streaming_stopped" {
		t.Fatalf("stop: unexpected status %v", out["status"])
	}
	m = readMsg()
	if m["type"] != "stream_end" {
		t.Fatalf("agent got wrong stop message: %v", m["type"])
	}

	// 3. Interval clamping.
	out = post("/api/devices/"+deviceID+"/stream", `{"interval": 5000}`, 200)
	if iv, _ := out["interval"].(float64); iv != 60 {
		t.Fatalf("clamp high: got %v want 60", out["interval"])
	}
	out = post("/api/devices/"+deviceID+"/stream", `{"interval": -5}`, 200)
	if iv, _ := out["interval"].(float64); iv != 1 {
		t.Fatalf("clamp low: got %v want 1", out["interval"])
	}

	// 4. Offline device -> 503.
	conn.CloseNow()
	// Wait for the hub to unregister the client so the check is deterministic
	// (TCP close and the read-loop teardown race otherwise).
	deadline := time.Now().Add(3 * time.Second)
	for hub.IsDeviceConnected(deviceKey) {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for device to unregister")
		}
		time.Sleep(10 * time.Millisecond)
	}
	post("/api/devices/"+deviceID+"/stream", "", 503)

	// 5. Unknown device -> 404.
	post("/api/devices/00000000-0000-0000-0000-000000000000/stream", "", 404)
}

func TestStreamEndpointAuthSmoke(t *testing.T) {
	ts, _, _ := newTestServerWithHub(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/devices/x/stream", nil)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatalf("unauthenticated stream: got %d want 401", r.StatusCode)
	}
}
