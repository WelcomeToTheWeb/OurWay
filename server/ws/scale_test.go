package ws

import (
	"context"
	"encoding/json"
	"fmt"

	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"nhooyr.io/websocket"

	"ourway/server/auth"
	"ourway/server/store"
)

// TestHubScaleSmoke is a bounded load gate (CI-sized): a fleet of simulated
// agents registers, connects, and pushes heartbeats + metrics through the
// real hub and SQLite store. It catches O(n^2) broadcast behavior and
// connection-table contention regressions at a scale that still runs in
// seconds. The heavyweight 10k-device harness in server/loadtest/scale
// stays a manual, opt-in tool.
func TestHubScaleSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scale smoke in -short mode")
	}
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
	go hub.Run()
	router := gin.New()
	router.GET("/ws", func(c *gin.Context) {
		hub.ServeHTTP(c, st, jwtAuth)
	})
	ts := httptest.NewServer(router)
	defer func() {
		ts.Close()
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}()

	const fleetSize = 25
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	// Register devices directly in the store (registration throughput is
	// HTTP-side and not what this gate measures).
	keys := make([]string, fleetSize)
	for i := range fleetSize {
		key := fmt.Sprintf("scale-key-%d", i)
		keys[i] = key
		addDevice(t, st, fmt.Sprintf("scale-%d", i), key)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect the fleet and push one heartbeat + one metrics frame each.
	var wg sync.WaitGroup
	errCh := make(chan error, fleetSize)
	for _, key := range keys {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
				Subprotocols: []string{"ourway-auth", key},
			})
			if err != nil {
				errCh <- fmt.Errorf("dial %s: %w", key, err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "done")
			for _, msgType := range []string{"heartbeat", "metrics"} {
				payload := map[string]interface{}{"device_key": key}
				if msgType == "metrics" {
					payload["data"] = map[string]interface{}{"cpu": 50.0, "ram": 60.0}
				}
				data, _ := json.Marshal(Message{Type: msgType, Payload: payload})
				if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
					errCh <- fmt.Errorf("write %s: %w", msgType, err)
					return
				}
			}
		}(key)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	// Every device's metrics must have been persisted.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		persisted := 0
		for i := range fleetSize {
			if _, err := st.MetricHistory.QueryLatestByDevice(fmt.Sprintf("scale-%d-id", i)); err == nil {
				persisted++
			}
		}
		if persisted == fleetSize {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("not all fleet metrics were persisted before the deadline")
}
