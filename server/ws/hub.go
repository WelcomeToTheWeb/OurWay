package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/gin-gonic/gin"
	"nhooyr.io/websocket"

	"ourway/server/auth"
	"ourway/server/store"
)

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
			if _, ok := h.clients[client.ID]; ok {
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

// SendToUser sends a message to a specific user by their token.
// If the user is not on this instance, it publishes to Redis.
func (h *Hub) SendToUser(token string, msgType string, payload interface{}) error {
	clientID := "user:" + token
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
			return fmt.Errorf("user %s send buffer full", token)
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

	return fmt.Errorf("user %s not connected", token)
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
	} else if token != "" {
		clientType = "user"
		clientID = "user:" + token
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

	h.register <- client
	defer func() {
		h.unregister <- client
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
		_, data, err := conn.Read(ctx)
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
			_ = jwtAuth
		}
	}
}

func (h *Hub) handleDeviceMessage(client *Client, msg Message, store *store.Store, deviceKey string) {
	switch msg.Type {
	case "heartbeat":
		dev, err := store.Devices.GetByKey(deviceKey)
		if err != nil {
			return
		}
		if err := store.Devices.UpdateLastSeen(dev.ID); err != nil {
			log.Printf("ws: heartbeat update failed: %v", err)
		}
	case "metrics":
		// Broadcast metrics to all users
		h.BroadcastMessage("metrics", msg.Payload)
	case "status":
		h.BroadcastMessage("status", msg.Payload)
	}
}
