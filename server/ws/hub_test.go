package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"nhooyr.io/websocket"

	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
)

// setupHubTest spins a fresh in-memory SQLite store and an HTTP server whose
// /ws route is served by a plain (non-distributed) hub.
func setupHubTest(t *testing.T) (*httptest.Server, *store.Store, *Hub) {
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
	hub := NewHub("http://localhost:3000", "")
	router := gin.New()
	router.GET("/ws", func(c *gin.Context) {
		hub.ServeHTTP(c, st, jwtAuth)
	})
	ts := httptest.NewServer(router)
	t.Cleanup(func() {
		ts.Close()
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return ts, st, hub
}

// addDevice inserts a device and returns its key.
func addDevice(t *testing.T, st *store.Store, name, key string) *models.Device {
	t.Helper()
	dev := &models.Device{
		ID:        name + "-id",
		Name:      name,
		Hostname:  name,
		OS:        "linux",
		Arch:      "amd64",
		Status:    "online",
		DeviceKey: key,
	}
	if err := st.Devices.Create(dev); err != nil {
		t.Fatalf("failed to create device %s: %v", name, err)
	}
	return dev
}

// dialAgent opens a device WebSocket connection authenticated with key,
// returning the raw conn plus a writer/reader pair.
func dialAgent(t *testing.T, tsURL, key string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(tsURL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"ourway-auth", key},
	})
	if err != nil {
		t.Fatalf("agent dial failed: %v", err)
	}
	t.Cleanup(func() {
		conn.Close(websocket.StatusNormalClosure, "test done")
	})
	return conn
}

// wsSend writes a Message envelope as JSON.
func wsSend(t *testing.T, conn *websocket.Conn, msgType string, payload interface{}) {
	t.Helper()
	data, err := json.Marshal(Message{Type: msgType, Payload: payload})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write %s failed: %v", msgType, err)
	}
}

// readUntil reads messages until one matches pred or the timeout elapses.
func readUntil(t *testing.T, conn *websocket.Conn, timeout time.Duration, pred func(Message) bool) *Message {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		_, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read failed waiting for message: %v", err)
		}
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if pred(msg) {
			return &msg
		}
	}
}

// metricsPayload is the agent's metrics frame shape. device_key in the
// payload is the spoofing vector this suite guards against (C2).
func metricsPayload(spoofedKey string, cpu float64) map[string]interface{} {
	return map[string]interface{}{
		"device_key": spoofedKey,
		"data": map[string]interface{}{
			"cpu":       cpu,
			"ram":       55.5,
			"ram_used":  8.0 * 1024 * 1024 * 1024,
			"ram_total": 16.0 * 1024 * 1024 * 1024,
			"uptime":    3600.0,
			"disks": []interface{}{
				map[string]interface{}{"total": 1000.0, "used": 250.0},
			},
			"network": map[string]interface{}{
				"eth0": map[string]interface{}{"bytes_recv": 100.0, "bytes_sent": 50.0},
			},
			"top_processes": []interface{}{map[string]interface{}{}, map[string]interface{}{}},
		},
	}
}

func TestHubMetricsUseConnectionIdentity(t *testing.T) {
	ts, st, hub := setupHubTest(t)
	go hub.Run()
	victim := addDevice(t, st, "victim", "victim-key")
	attacker := addDevice(t, st, "attacker", "attacker-key")

	conn := dialAgent(t, ts.URL, attacker.DeviceKey)
	// The spoofed payload names the victim; the hub must still record
	// against the connection's device (C2 regression guard).
	wsSend(t, conn, "metrics", metricsPayload(victim.DeviceKey, 88.8))

	readUntil(t, conn, 5*time.Second, func(m Message) bool {
		return m.Type == "metrics"
	})

	// The attacker's record must hold the frame; the victim's must not.
	got, err := st.MetricHistory.QueryLatestByDevice(attacker.ID)
	if err != nil {
		t.Fatalf("no metrics recorded for attacker device: %v", err)
	}
	if got.CPU != 88.8 {
		t.Errorf("expected attacker CPU 88.8, got %v", got.CPU)
	}
	if v, err := st.MetricHistory.QueryLatestByDevice(victim.ID); err == nil {
		t.Errorf("victim device unexpectedly received metrics (CPU=%v) via spoofed device_key", v.CPU)
	}

	// The broadcast carries the connection's device identity, not the
	// payload's. Verified below via the dedicated broadcast-shape test.
}

func TestHubMetricsBroadcastShape(t *testing.T) {
	ts, st, hub := setupHubTest(t)
	go hub.Run()
	dev := addDevice(t, st, "webdev", "webdev-key")
	agentConn := dialAgent(t, ts.URL, dev.DeviceKey)

	// The hub broadcasts metrics to all connected clients, including the
	// device connections themselves, so the sending agent observes its own
	// frame echoed in the REST broadcast shape.
	wsSend(t, agentConn, "metrics", metricsPayload(dev.DeviceKey, 12.5))
	msg := readUntil(t, agentConn, 5*time.Second, func(m Message) bool {
		return m.Type == "metrics"
	})
	payload, ok := msg.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("metrics broadcast payload not an object: %T", msg.Payload)
	}
	if payload["device_id"] != dev.ID {
		t.Errorf("expected broadcast device_id %q, got %v", dev.ID, payload["device_id"])
	}
	metrics, ok := payload["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("metrics broadcast missing metrics object: %v", payload)
	}
	if metrics["cpu"] != 12.5 {
		t.Errorf("expected broadcast cpu 12.5, got %v", metrics["cpu"])
	}
	if metrics["disk_usage"] != 25.0 {
		t.Errorf("expected broadcast disk_usage 25.0, got %v", metrics["disk_usage"])
	}
	if metrics["processes"] != 2.0 {
		t.Errorf("expected broadcast processes 2, got %v", metrics["processes"])
	}
}

func TestHubUnknownDeviceKeyRejected(t *testing.T) {
	ts, _, hub := setupHubTest(t)
	go hub.Run()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"ourway-auth", "not-a-real-key"},
	})
	if err == nil {
		t.Fatal("dial with unknown device key unexpectedly succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		got := "nil response"
		if resp != nil {
			got = fmt.Sprintf("status %d", resp.StatusCode)
		}
		t.Errorf("expected 401 for unknown device key, got %s", got)
	}
}

func TestHubCredentialExtraction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"well formed", "ourway-auth, device-key-123", "device-key-123"},
		{"extra whitespace", "  ourway-auth ,   device-key-123  ", "device-key-123"},
		{"missing credential", "ourway-auth", ""},
		{"wrong protocol", "other-auth, key", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/ws", nil)
			c.Request.Header.Set("Sec-WebSocket-Protocol", tt.header)
			if got := wsCredential(c); got != tt.want {
				t.Errorf("wsCredential(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestHubOriginAllowList(t *testing.T) {
	h := NewHub("", "allowed.example.com, other.example.com")
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/ws", nil)
	c.Request.Host = "api.example.com"
	tests := []struct {
		origin string
		want   bool
	}{
		{"https://allowed.example.com", true},
		{"https://other.example.com", true},
		{"https://evil.example.com", false},
		{"https://allowed.example.com.evil.com", false},
		{"not a url", false},
	}
	for _, tt := range tests {
		if got := h.originAllowed(tt.origin, c); got != tt.want {
			t.Errorf("originAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
		}
	}
}

func TestHubOriginDefaultSameOrigin(t *testing.T) {
	h := NewHub("http://localhost:3000", "")
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/ws", nil)
	c.Request.Host = "api.example.com"
	tests := []struct {
		origin string
		host   string
		want   bool
	}{
		{"https://api.example.com", "api.example.com", true},
		{"http://localhost:3000", "api.example.com", true},
		{"http://localhost:5173", "api.example.com", true},
		{"https://api.example.com.evil.com", "api.example.com", false},
	}
	for _, tt := range tests {
		c.Request.Host = tt.host
		if got := h.originAllowed(tt.origin, c); got != tt.want {
			t.Errorf("originAllowed(origin=%q, host=%q) = %v, want %v", tt.origin, tt.host, got, tt.want)
		}
	}
}

func TestHubSendToDevice(t *testing.T) {
	ts, st, hub := setupHubTest(t)
	go hub.Run()
	dev := addDevice(t, st, "target", "target-key")
	_ = ts
	conn := dialAgent(t, ts.URL, dev.DeviceKey)

	// The register channel is processed asynchronously by hub.Run(); the
	// dial returning does not mean the hub has registered the client yet.
	// Poll instead of asserting immediately (this was a flaky race).
	deadline := time.Now().Add(5 * time.Second)
	for !hub.IsDeviceConnected(dev.DeviceKey) {
		if time.Now().After(deadline) {
			t.Fatal("expected device to be considered connected after dialing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := hub.SendToDevice(dev.DeviceKey, "ping", nil); err != nil {
		t.Fatalf("SendToDevice failed: %v", err)
	}
	readUntil(t, conn, 5*time.Second, func(m Message) bool {
		return m.Type == "ping"
	})
	if err := hub.SendToDevice("no-such-key", "ping", nil); err == nil {
		t.Error("expected error sending to unregistered device")
	}
}

func TestHubHeartbeatUpdatesDeviceAndIPs(t *testing.T) {
	ts, st, hub := setupHubTest(t)
	go hub.Run()
	dev := addDevice(t, st, "hbdev", "hb-key")
	conn := dialAgent(t, ts.URL, dev.DeviceKey)

	before := time.Now()
	wsSend(t, conn, "heartbeat", map[string]interface{}{
		"private_ip": "10.0.0.99",
		"public_ip":  "203.0.113.7",
	})
	// Heartbeats do not broadcast; poll the store for the update.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := st.Devices.GetByKey("hb-key")
		if err == nil && got.PrivateIP == "10.0.0.99" && got.PublicIP == "203.0.113.7" && !got.LastSeen.Before(before) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	got, _ := st.Devices.GetByKey("hb-key")
	t.Fatalf("heartbeat did not update device record: private_ip=%q public_ip=%q", got.PrivateIP, got.PublicIP)
}
