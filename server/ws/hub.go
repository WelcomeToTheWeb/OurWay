package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"nhooyr.io/websocket"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/events"
	"ourway/server/models"
	"ourway/server/store"
)

// toFloat safely converts interface{} to float64
func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case uint64:
		return float64(val)
	default:
		return 0
	}
}

// Client is a registered WebSocket client.
type Client struct {
	ID        string
	Type      string // "user" or "device"
	DeviceKey string
	Conn      *websocket.Conn
	SendCh    chan []byte
}

// Message is the envelope for WebSocket messages.
type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// RedisPubSub abstracts Redis pub/sub operations for the hub.
type RedisPubSub struct {
	Publish   func(topic string, message []byte) error
	Subscribe func(topic string, handler func([]byte)) error
}

// Hub manages WebSocket connections and message broadcasting.
type Hub struct {
	clients    map[string]*Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	mu         sync.RWMutex
	redis      *RedisPubSub // Optional Redis pub/sub for distributed broadcasting
	reaperOnce sync.Once
	Alerts     *alerts.Engine // Optional alert engine, evaluated on the WS metrics path
}

// NewHub creates a new WebSocket hub.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client, 100),
		unregister: make(chan *Client, 100),
		broadcast:  make(chan []byte, 100),
	}
}

// NewDistributedHub creates a new distributed WebSocket hub with Redis pub/sub support.
func NewDistributedHub(redis *RedisPubSub) *Hub {
	h := &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client, 1000),
		unregister: make(chan *Client, 1000),
		broadcast:  make(chan []byte, 1000),
		redis:      redis,
	}

	// Subscribe to cross-instance broadcasts
	go func() {
		err := redis.Subscribe("ws:broadcast", func(data []byte) {
			h.handleRemoteBroadcast(data)
		})
		if err != nil {
			log.Printf("ws: failed to subscribe to broadcast channel: %v", err)
		}
	}()

	// Subscribe to targeted messages for clients on this instance
	go func() {
		err := redis.Subscribe("ws:targeted", func(data []byte) {
			h.handleRemoteTargeted(data)
		})
		if err != nil {
			log.Printf("ws: failed to subscribe to targeted channel: %v", err)
		}
	}()

	return h
}

func (h *Hub) handleRemoteTargeted(data []byte) {
	var msg RemoteMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	h.mu.RLock()
	client, ok := h.clients[msg.Target]
	h.mu.RUnlock()

	if !ok {
		return
	}

	select {
	case client.SendCh <- msg.Data:
	default:
	}
}

func (h *Hub) handleRemoteBroadcast(data []byte) {
	h.mu.RLock()
	for _, client := range h.clients {
		select {
		case client.SendCh <- data:
		default:
		}
	}
	h.mu.RUnlock()
}

// Run starts the hub's main loop.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			h.mu.Unlock()
			log.Printf("ws: client %s registered (%s)", client.ID, client.Type)
		case client := <-h.unregister:
			h.mu.Lock()
			// Only unregister if the currently registered client for this
			// ID is exactly this connection. A stale conn's unregister must
			// not delete a newer conn that already registered under the same
			// ID (e.g. a device reconnecting).
			if current, ok := h.clients[client.ID]; ok && current == client {
				delete(h.clients, client.ID)
				close(client.SendCh)
			}
			h.mu.Unlock()
			log.Printf("ws: client %s unregistered (%s)", client.ID, client.Type)
		case msg := <-h.broadcast:
			// Deliver to local clients
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.SendCh <- msg:
				default:
					// Drop slow clients
				}
			}
			h.mu.RUnlock()

			// Publish to Redis for other instances
			if h.redis != nil {
				if err := h.redis.Publish("ws:broadcast", msg); err != nil {
					log.Printf("ws: failed to publish broadcast: %v", err)
				}
			}
		}
	}
}

// BroadcastMessage sends a message to all connected clients across all instances.
func (h *Hub) BroadcastMessage(msgType string, payload interface{}) {
	msg := Message{Type: msgType, Payload: payload}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("ws: failed to marshal message: %v", err)
		return
	}
	select {
	case h.broadcast <- data:
	default:
		// If local buffer is full, publish directly to Redis
		if h.redis != nil {
			h.redis.Publish("ws:broadcast", data)
		}
	}
}

// SendToDevice sends a message to a specific device by its device key.
// If the device is not on this instance, it publishes to Redis.
func (h *Hub) SendToDevice(deviceKey string, msgType string, payload interface{}) error {
	clientID := "device:" + deviceKey
	msg := Message{Type: msgType, Payload: payload}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	h.mu.RLock()
	client, ok := h.clients[clientID]
	h.mu.RUnlock()

	if ok {
		select {
		case client.SendCh <- data:
			return nil
		default:
			return fmt.Errorf("device %s send buffer full", deviceKey)
		}
	}

	// Device not on this instance, publish to Redis
	if h.redis != nil {
		remoteMsg := RemoteMessage{Target: clientID, Data: data}
		remoteData, err := json.Marshal(remoteMsg)
		if err != nil {
			return err
		}
		if err := h.redis.Publish("ws:targeted", remoteData); err != nil {
			return fmt.Errorf("failed to publish targeted message: %w", err)
		}
		return nil
	}

	return fmt.Errorf("device %s not connected", deviceKey)
}

// SendToUser sends a message to a specific user by their user ID.
// If the user is not on this instance, it publishes to Redis.
func (h *Hub) SendToUser(userID string, msgType string, payload interface{}) error {
	clientID := "user:" + userID
	msg := Message{Type: msgType, Payload: payload}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	h.mu.RLock()
	client, ok := h.clients[clientID]
	h.mu.RUnlock()

	if ok {
		select {
		case client.SendCh <- data:
			return nil
		default:
			return fmt.Errorf("user %s send buffer full", userID)
		}
	}

	// User not on this instance, publish to Redis
	if h.redis != nil {
		remoteMsg := RemoteMessage{Target: clientID, Data: data}
		remoteData, err := json.Marshal(remoteMsg)
		if err != nil {
			return err
		}
		if err := h.redis.Publish("ws:targeted", remoteData); err != nil {
			return fmt.Errorf("failed to publish targeted message: %w", err)
		}
		return nil
	}

	return fmt.Errorf("user %s not connected", userID)
}

// RemoteMessage is a message sent to a specific client on another instance.
type RemoteMessage struct {
	Target string `json:"target"`
	Data   []byte `json:"data"`
}

// IsDeviceConnected checks if a device is currently connected.
func (h *Hub) IsDeviceConnected(deviceKey string) bool {
	clientID := "device:" + deviceKey
	h.mu.RLock()
	_, ok := h.clients[clientID]
	h.mu.RUnlock()
	return ok
}

// ServeHTTP handles a WebSocket upgrade and manages the client connection.
func (h *Hub) ServeHTTP(c *gin.Context, store *store.Store, jwtAuth *auth.JWTAuth) {
	// Parse query parameters for auth
	deviceKey := c.Query("device_key")
	token := c.Query("token")

	var clientID string
	var clientType string

	if deviceKey != "" {
		clientType = "device"
		clientID = "device:" + deviceKey
		// Reject unknown device keys before accepting the connection.
		if _, err := store.Devices.GetByKey(deviceKey); err != nil {
			c.JSON(401, gin.H{"error": "invalid device key"})
			return
		}
	} else if token != "" {
		clientType = "user"
		// Reject invalid/expired access tokens before accepting the
		// connection — previously any non-empty token was accepted.
		// The client is registered under the user's stable ID (not the
		// rotating JWT) so messages can be targeted by user ID.
		claims, err := jwtAuth.ValidateToken(token)
		if err != nil {
			c.JSON(401, gin.H{"error": "invalid token"})
			return
		}
		clientID = "user:" + claims.UserID
	} else {
		c.JSON(400, gin.H{"error": "provide device_key or token query parameter"})
		return
	}

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		log.Printf("ws: accept failed: %v", err)
		c.JSON(400, gin.H{"error": "websocket accept failed"})
		return
	}

	client := &Client{
		ID:        clientID,
		Type:      clientType,
		DeviceKey: deviceKey,
		Conn:      conn,
		SendCh:    make(chan []byte, 100),
	}

	// Ensure the stale-device reaper is running (no-op after the first start)
	if clientType == "device" {
		h.startReaper(store)
	}

	h.register <- client
	defer func() {
		h.unregister <- client
		if clientType == "device" && deviceKey != "" {
			// The device's client is going away: mark it offline so presence
			// reflects reality (the next heartbeat can then fire device_online).
			if dev, err := store.Devices.GetByKey(deviceKey); err == nil {
				dev.Status = "offline"
				if err := store.Devices.Update(dev); err != nil {
					log.Printf("ws: failed to mark device offline: %v", err)
				}
			}
		}
		conn.Close(websocket.StatusNormalClosure, "closing")
	}()

	// Sender goroutine
	go func() {
		ctx := context.Background()
		for data := range client.SendCh {
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				return
			}
		}
	}()

	// Receiver loop
	ctx := c.Request.Context()
	for {
		// Devices are expected to send a 15s heartbeat; drop a device
		// connection that goes silent for 60s (e.g. blackholed peer) so the
		// client goroutines don't leak. User (browser) connections are
		// receive-only and stay open without a deadline.
		readCtx := ctx
		var cancelRead context.CancelFunc
		if clientType == "device" {
			readCtx, cancelRead = context.WithTimeout(ctx, 60*time.Second)
		}
		_, data, err := conn.Read(readCtx)
		if cancelRead != nil {
			cancelRead()
		}
		if err != nil {
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		// Route messages based on client type
		switch clientType {
		case "device":
			h.handleDeviceMessage(client, msg, store, deviceKey)
		case "user":
			// User messages are echoed or handled for future use
		}
	}
}

// startReaper launches a single background goroutine that marks devices
// offline when their last_seen is older than ~3x the agent heartbeat interval.
func (h *Hub) startReaper(store *store.Store) {
	h.reaperOnce.Do(func() {
		go func() {
			const reaperInterval = 30 * time.Second
			const staleAfter = 45 * time.Second // ~3x the 15s agent heartbeat
			ticker := time.NewTicker(reaperInterval)
			defer ticker.Stop()
			for range ticker.C {
				devices, err := store.Devices.ListAll()
				if err != nil {
					continue
				}
				cutoff := time.Now().Add(-staleAfter)
				for i := range devices {
					d := &devices[i]
					if d.Status == "online" && d.LastSeen.Before(cutoff) {
						d.Status = "offline"
						if err := store.Devices.Update(d); err != nil {
							log.Printf("ws: failed to mark stale device offline: %v", err)
							continue
						}
						log.Printf("ws: device %s marked offline (stale last_seen)", d.ID)
						events.Publish("device_offline", map[string]interface{}{
							"device_id": d.ID,
							"name":      d.Name,
						})
					}
				}
			}
		}()
	})
}

func (h *Hub) handleDeviceMessage(_ *Client, msg Message, store *store.Store, deviceKey string) {
	switch msg.Type {
	case "heartbeat":
		dev, err := store.Devices.GetByKey(deviceKey)
		if err != nil {
			return
		}
		wasOffline := dev.Status == "offline"
		if err := store.Devices.UpdateLastSeen(dev.ID); err != nil {
			log.Printf("ws: heartbeat update failed: %v", err)
		}
		if wasOffline {
			events.Publish("device_online", map[string]interface{}{
				"device_id": dev.ID,
				"name":      dev.Name,
			})
		}
	case "metrics":
		// Extract and save metrics to database
		if payloadMap, ok := msg.Payload.(map[string]interface{}); ok {
			if dk, ok := payloadMap["device_key"].(string); ok && dk != "" {
				dev, err := store.Devices.GetByKey(dk)
				if err == nil {
					if dataVal, ok := payloadMap["data"].(map[string]interface{}); ok {
						mh := &models.MetricHistory{
							DeviceID:  dev.ID,
							Timestamp: time.Now(),
						}
						if v, ok := dataVal["cpu"].(float64); ok {
							mh.CPU = v
						}
						if v, ok := dataVal["ram"].(float64); ok {
							mh.RAM = v
						}
						if v, ok := dataVal["ram_used"].(float64); ok {
							mh.RAMUsed = uint64(v)
						}
						if v, ok := dataVal["ram_total"].(float64); ok {
							mh.RAMTotal = uint64(v)
						}
						if v, ok := dataVal["uptime"].(float64); ok {
							mh.Uptime = uint64(v)
						}
						// Calculate disk usage from disks array
						if disks, ok := dataVal["disks"].([]interface{}); ok && len(disks) > 0 {
							var totalDisk, usedDisk float64
							for _, d := range disks {
								if dm, ok := d.(map[string]interface{}); ok {
									totalDisk += toFloat(dm["total"])
									usedDisk += toFloat(dm["used"])
								}
							}
							mh.DiskTotal = uint64(totalDisk)
							mh.DiskUsed = uint64(usedDisk)
							if totalDisk > 0 {
								mh.DiskUsage = (usedDisk / totalDisk) * 100
							}
						}
						// Network totals
						if net, ok := dataVal["network"].(map[string]interface{}); ok {
							var netIn, netOut float64
							for _, ni := range net {
								if nm, ok := ni.(map[string]interface{}); ok {
									netIn += toFloat(nm["bytes_recv"])
									netOut += toFloat(nm["bytes_sent"])
								}
							}
							mh.NetIn = uint64(netIn)
							mh.NetOut = uint64(netOut)
						}
						if procs, ok := dataVal["top_processes"].([]interface{}); ok {
							mh.Processes = len(procs)
						}
						if store.MetricHistory != nil {
							if err := store.MetricHistory.Insert(mh); err != nil {
								log.Printf("ws: failed to save metrics: %v", err)
							}
						}

						// Evaluate alerts on the WS metrics path, same as the REST
						// path — agents report metrics over WebSocket, so without
						// this the alert engine never runs in the real flow.
						if h.Alerts != nil {
							h.Alerts.Evaluate(models.Metrics{
								CPU:       mh.CPU,
								RAM:       mh.RAM,
								RAMUsed:   mh.RAMUsed,
								RAMTotal:  mh.RAMTotal,
								DiskUsage: mh.DiskUsage,
								DiskUsed:  mh.DiskUsed,
								DiskTotal: mh.DiskTotal,
								NetIn:     mh.NetIn,
								NetOut:    mh.NetOut,
								Uptime:    mh.Uptime,
								Processes: mh.Processes,
							}, dev.ID, dev.Name)
						}

						// Broadcast in the REST shape ({device_id, device, metrics})
						// the web client's flatten() expects, not the raw agent payload
						h.BroadcastMessage("metrics", map[string]interface{}{
							"device_id": dev.ID,
							"device":    dev.Name,
							"metrics": map[string]interface{}{
								"cpu":        mh.CPU,
								"ram":        mh.RAM,
								"ram_used":   mh.RAMUsed,
								"ram_total":  mh.RAMTotal,
								"disk_usage": mh.DiskUsage,
								"disk_used":  mh.DiskUsed,
								"disk_total": mh.DiskTotal,
								"net_in":     mh.NetIn,
								"net_out":    mh.NetOut,
								"uptime":     mh.Uptime,
								"processes":  mh.Processes,
							},
						})
					}
					_ = store.Devices.UpdateLastSeen(dev.ID)
				}
			}
		}
	case "status":
		h.BroadcastMessage("status", msg.Payload)
	}
}
