package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"nhooyr.io/websocket"

	"ourway/server/alerts"
	"ourway/server/auth"
	"ourway/server/config"
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

// RedisPubSub abstracts Redis pub/sub operations for the hub. The
// optional presence functions (SetEX/Get/Del) back cross-instance device
// presence (H2); when nil the hub degrades to local-only checks.
type RedisPubSub struct {
	Publish   func(topic string, message []byte) error
	Subscribe func(topic string, handler func([]byte)) error
	SetEX     func(key string, value string, ttl time.Duration) error
	Get       func(key string, out *string) error
	Del       func(key string) error
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

	// instanceID uniquely identifies this server instance so Redis
	// broadcasts can suppress their own loopback (M8).
	instanceID string

	// webURL / wsOrigins bound the allowed browser Origins for the WS
	// endpoint (C5); agents send no Origin header.
	webURL    string
	wsOrigins []string

	// alertEval decouples alert evaluation from the connection read path (M5).
	alertEval chan alertJob

	// dropped counts messages dropped because a client's send buffer or a
	// hub channel was full (M10/M8).
	dropped atomic.Uint64
}

// alertJob is one deferred alert-engine evaluation.
type alertJob struct {
	metrics    models.Metrics
	deviceID   string
	deviceName string
}

// NewHub creates a new WebSocket hub.
func NewHub(webURL, wsOrigins string) *Hub {
	h := &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client, 100),
		unregister: make(chan *Client, 100),
		broadcast:  make(chan []byte, 100),
		instanceID: uuid.New().String(),
		alertEval:  make(chan alertJob, 256),
		webURL:     webURL,
		wsOrigins:  splitOrigins(wsOrigins),
	}
	h.startAlertWorker()
	return h
}

// NewDistributedHub creates a new distributed WebSocket hub with Redis pub/sub support.
func NewDistributedHub(redis *RedisPubSub, webURL, wsOrigins string) *Hub {
	h := &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client, 1000),
		unregister: make(chan *Client, 1000),
		broadcast:  make(chan []byte, 1000),
		redis:      redis,
		instanceID: uuid.New().String(),
		alertEval:  make(chan alertJob, 256),
		webURL:     webURL,
		wsOrigins:  splitOrigins(wsOrigins),
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

	h.startAlertWorker()
	return h
}

// startAlertWorker runs the single consumer of h.alertEval. Alert
// evaluation does its own DB writes; keeping it off the connection read
// path means a slow database can't stall heartbeats (M5).
func (h *Hub) startAlertWorker() {
	go func() {
		for job := range h.alertEval {
			if h.Alerts == nil {
				continue
			}
			h.Alerts.Evaluate(job.metrics, job.deviceID, job.deviceName)
		}
	}()
}

// DroppedCount reports how many messages the hub dropped on full buffers
// (M10). Useful in health endpoints and tests.
func (h *Hub) DroppedCount() uint64 {
	return h.dropped.Load()
}

func (h *Hub) handleRemoteTargeted(data []byte) {
	var msg RemoteMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	for _, client := range h.clientsFor(msg.Target) {
		select {
		case client.SendCh <- msg.Data:
		default:
			h.dropped.Add(1)
		}
	}
}

// clientsFor returns the connections addressed by target: the exact
// client ID, plus (for "user:<id>") every browser connection that user has
// open. A user may have several (tabs, the device page and its session
// viewer), and a message for the user must reach all of them.
func (h *Hub) clientsFor(target string) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []*Client
	if c, ok := h.clients[target]; ok {
		out = append(out, c)
	}
	prefix := target + "#"
	for id, c := range h.clients {
		if strings.HasPrefix(id, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (h *Hub) handleRemoteBroadcast(data []byte) {
	var env RemoteBroadcast
	if err := json.Unmarshal(data, &env); err != nil {
		return
	}
	// Suppress loopback: Redis echoes our own publish back to us and we
	// already delivered it to local clients (M8).
	if env.Src == h.instanceID {
		return
	}
	h.mu.RLock()
	for _, client := range h.clients {
		select {
		case client.SendCh <- env.Data:
		default:
			h.dropped.Add(1)
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
					// Drop slow clients (M10: counted so the degradation
					// is not invisible).
					h.dropped.Add(1)
				}
			}
			h.mu.RUnlock()

			// Publish to Redis for other instances. Fire-and-forget: run in
			// a goroutine so a slow/stalled Redis can never block the Run
			// loop (and thus register/unregister/broadcast processing).
			if h.redis != nil {
				env, err := json.Marshal(RemoteBroadcast{Src: h.instanceID, Data: msg})
				if err != nil {
					log.Printf("ws: failed to marshal broadcast envelope: %v", err)
					break
				}
				go func() {
					if err := h.redis.Publish("ws:broadcast", env); err != nil {
						log.Printf("ws: failed to publish broadcast: %v", err)
					}
				}()
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
		// Local buffer full: publish directly to Redis (other instances
		// still get it), or count the drop when Redis is absent (M8).
		if h.redis != nil {
			env, err := json.Marshal(RemoteBroadcast{Src: h.instanceID, Data: data})
			if err == nil {
				if err := h.redis.Publish("ws:broadcast", env); err != nil {
					log.Printf("ws: failed to publish broadcast: %v", err)
				}
			} else {
				log.Printf("ws: failed to marshal broadcast envelope: %v", err)
			}
		} else {
			h.dropped.Add(1)
		}
	}
}

// SendToDevice sends a message to a specific device by its device key.
// If the device is not on this instance, it publishes to Redis.
func (h *Hub) SendToDevice(deviceKey string, msgType string, payload interface{}) error {
	msg := Message{Type: msgType, Payload: payload}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Session messages (input, quality) prefer the per-session
	// remote-control executable when it is connected: it owns capture
	// and input during the session. The agent connection is the
	// fallback and also receives stream/reboot/maintenance messages.
	// session_end is delivered to both: the exe must stop streaming and
	// the agent must clear its own session state so the next
	// session_start is not ignored as "already active".
	h.mu.RLock()
	clients := make([]*Client, 0, 2)
	if client, ok := h.clients["remote:"+deviceKey]; ok {
		clients = append(clients, client)
	}
	_, haveRemote := h.clients["remote:"+deviceKey]
	deviceClient, haveDevice := h.clients["device:"+deviceKey]
	// Input goes to the exe alone when it is connected: the agent
	// service runs in Session 0, where SendInput fails with "Access is
	// denied". Other messages (quality, session_end) still reach the
	// agent so it keeps its own session state in step.
	if haveDevice && !(haveRemote && msgType == "input") {
		clients = append(clients, deviceClient)
	}
	h.mu.RUnlock()

	if len(clients) > 0 {
		delivered := false
		for _, client := range clients {
			select {
			case client.SendCh <- data:
				delivered = true
			default:
				h.dropped.Add(1)
			}
		}
		if delivered {
			return nil
		}
		return fmt.Errorf("device %s send buffer full", deviceKey)
	}
	clientID := "device:" + deviceKey

	// Device not on this instance. In distributed mode, verify it is
	// connected to *some* instance via the presence key before claiming
	// delivery (H2) — publishing to Redis for a device connected nowhere
	// used to report false success.
	if h.redis != nil {
		if h.redis.Get != nil {
			var v string
			if err := h.redis.Get(devicePresenceKey(deviceKey), &v); err != nil || v == "" {
				return fmt.Errorf("device %s not connected", deviceKey)
			}
		}
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

	if clients := h.clientsFor(clientID); len(clients) > 0 {
		delivered := false
		for _, client := range clients {
			select {
			case client.SendCh <- data:
				delivered = true
			default:
				h.dropped.Add(1)
			}
		}
		if delivered {
			return nil
		}
		return fmt.Errorf("user %s send buffer full", userID)
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

// RemoteBroadcast wraps a broadcast for cross-instance delivery. Src lets
// the originating instance suppress its own loopback (M8).
type RemoteBroadcast struct {
	Src  string `json:"src"`
	Data []byte `json:"data"`
}

// devicePresenceKey is the Redis key marking a device as connected to some
// instance. The value is the connection's unique ID; the owner refreshes it
// while alive and deletes it (compare-and-delete) on disconnect (H2).
func devicePresenceKey(deviceKey string) string {
	return "ws:device:" + deviceKey
}

// IsDeviceConnected checks if a device is currently connected, locally or
// (in distributed mode) to another instance via the presence key (H2).
func (h *Hub) IsDeviceConnected(deviceKey string) bool {
	clientID := "device:" + deviceKey
	h.mu.RLock()
	_, ok := h.clients[clientID]
	h.mu.RUnlock()
	if ok {
		return true
	}
	if h.redis != nil && h.redis.Get != nil {
		var v string
		if err := h.redis.Get(devicePresenceKey(deviceKey), &v); err == nil && v != "" {
			return true
		}
	}
	return false
}

// ServeHTTP handles a WebSocket upgrade and manages the client connection.
func (h *Hub) ServeHTTP(c *gin.Context, store *store.Store, jwtAuth *auth.JWTAuth) {
	// C5: the credential travels in the Sec-WebSocket-Protocol list, not
	// in the query string (query strings end up in access logs, proxy
	// logs, and the Referer header). Clients propose
	// "ourway-auth, <credential>" where credential is a device key or a
	// user access token.
	credential, isRemote := wsCredential(c)
	if credential == "" {
		c.JSON(400, gin.H{"error": "provide a credential via Sec-WebSocket-Protocol: ourway-auth, <device key or token>"})
		return
	}

	// C5: restrict origins. Browsers must connect from the configured web
	// origin (or same-origin, or local dev); agent connections send no
	// Origin header and are unaffected.
	if origin := c.GetHeader("Origin"); origin != "" && !h.originAllowed(origin, c) {
		c.JSON(403, gin.H{"error": "origin not allowed"})
		return
	}

	var clientID string
	var clientType string
	var deviceKey string
	// remoteSession is set for a remote exe authenticated with a
	// per-session token; only those connections may stream frames.
	var remoteSession *models.Session

	if claims, err := jwtAuth.ValidateToken(credential); err == nil {
		// User (browser) connection: registered under the user's stable
		// ID (not the rotating JWT) so messages can be targeted by user ID.
		clientType = "user"
		// Browsers may hold several connections at once (tabs, the device
		// page plus its session viewer). Each gets its own unique ID so one
		// never supersedes another; messages for the user fan out to all
		// (see clientsFor).
		clientID = "user:" + claims.UserID + "#" + uuid.NewString()
	} else if sess, err := store.Sessions.GetLiveByTokenHash(models.HashRemoteToken(credential)); isRemote && err == nil {
		// The remote-control exe authenticates with a per-session token
		// (not the device key) and registers under the device's remote
		// ID so it coexists with the agent's own connection.
		dev, derr := store.Devices.GetByID(sess.DeviceID)
		if derr != nil {
			c.JSON(401, gin.H{"error": "invalid session token"})
			return
		}
		deviceKey = dev.DeviceKey
		clientType = "remote"
		clientID = "remote:" + dev.DeviceKey
		remoteSession = sess
	} else if _, err := store.Devices.GetByKey(credential); err == nil {
		// Reject unknown device keys before accepting the connection.
		// A device key with the remote marker is still accepted for
		// exes older than the per-session token; the agent's own
		// connection is never superseded by it.
		deviceKey = credential
		if isRemote {
			clientType = "remote"
			clientID = "remote:" + credential
		} else {
			clientType = "device"
			clientID = "device:" + credential
		}
	} else {
		c.JSON(401, gin.H{"error": "invalid token or device key"})
		return
	}

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		// Origin is checked manually above; the library check is a pass
		// through so the handshake succeeds for the agent (no Origin).
		OriginPatterns: []string{".*"},
		Subprotocols:   []string{"ourway-auth"},
	})
	if err != nil {
		log.Printf("ws: accept failed: %v", err)
		c.JSON(400, gin.H{"error": "websocket accept failed"})
		return
	}

	if remoteSession != nil {
		// Frames arrive as binary messages; the default 32 KiB read
		// limit would reject nearly every JPEG.
		conn.SetReadLimit(maxFrameBytes)
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

	// M7: a duplicate connection for the same identity supersedes the old
	// one; close the older conn so its stale metrics/heartbeats stop
	// feeding the hub before the next read deadline kills it.
	h.mu.RLock()
	if old, ok := h.clients[clientID]; ok {
		h.mu.RUnlock()
		log.Printf("ws: duplicate connection for %s, closing the older one", clientID)
		old.Conn.Close(websocket.StatusPolicyViolation, "superseded by newer connection")
	} else {
		h.mu.RUnlock()
	}

	// H2: cross-instance presence. The connection claims a per-device key
	// in Redis (refreshed while alive, compare-and-deleted on
	// disconnect) so IsDeviceConnected/SendToDevice work when the device
	// sits on another instance.
	connID := ""
	var presenceStop chan struct{}
	if clientType == "device" && deviceKey != "" && h.redis != nil && h.redis.SetEX != nil {
		connID = h.instanceID + "-" + uuid.New().String()
		if err := h.redis.SetEX(devicePresenceKey(deviceKey), connID, 90*time.Second); err != nil {
			log.Printf("ws: failed to set device presence: %v", err)
		}
		presenceStop = make(chan struct{})
		defer close(presenceStop)
		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-presenceStop:
					return
				case <-t.C:
					if err := h.redis.SetEX(devicePresenceKey(deviceKey), connID, 90*time.Second); err != nil {
						log.Printf("ws: failed to refresh device presence: %v", err)
					}
				}
			}
		}()
	}

	h.register <- client
	defer func() {
		h.unregister <- client
		if clientType == "remote" {
			// The remote-control exe leaving must not affect device
			// presence: the agent's connection owns that.
			return
		}
		if clientType == "device" && deviceKey != "" {
			// Only write "offline" if this connection is still the
			// registered one; the old conn's teardown must not knock a
			// newer conn for the same key offline (H2).
			h.mu.RLock()
			current := h.clients[clientID] == client
			h.mu.RUnlock()
			if current {
				h.clearDevicePresence(deviceKey, connID)
				// The device's client is going away: mark it offline so
				// presence reflects reality (the next heartbeat can then
				// fire device_online).
				if dev, err := store.Devices.GetByKey(deviceKey); err == nil {
					dev.Status = "offline"
					if err := store.Devices.Update(dev); err != nil {
						log.Printf("ws: failed to mark device offline: %v", err)
					}
				}
			}
		}
		conn.Close(websocket.StatusNormalClosure, "closing")
	}()

	// H1: the server pings every connection. Browsers and the agent
	// answer pings automatically at the protocol level, so a blackholed
	// peer is detected and torn down without relying on either client to
	// keep the link alive.
	pingStop := make(chan struct{})
	defer close(pingStop)
	go func() {
		t := time.NewTicker(40 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-pingStop:
				return
			case <-t.C:
				pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := conn.Ping(pctx)
				cancel()
				if err != nil {
					conn.Close(websocket.StatusInternalError, "ping failed")
					return
				}
			}
		}
	}()

	// Sender goroutine. Each write gets its own timeout so a blackholed
	// connection (e.g. a sleeping laptop) can't block the writer forever.
	// On write failure we close the conn so the read loop unblocks and the
	// deferred unregister runs.
	go func() {
		for data := range client.SendCh {
			writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				conn.Close(websocket.StatusInternalError, "write failed")
				return
			}
		}
	}()

	// Receiver loop
	ctx := c.Request.Context()
	var fs frameState
	for {
		// Devices are expected to send a 15s heartbeat; drop a device
		// connection that goes silent for 60s (e.g. blackholed peer) so the
		// client goroutines don't leak. User (browser) connections are
		// receive-only except for the 30s app-level ping (H1); the 120s
		// read deadline (restarted by every successful read) is a safety
		// net for a blackholed peer.
		readCtx := ctx
		var cancelRead context.CancelFunc
		switch clientType {
		case "device":
			readCtx, cancelRead = context.WithTimeout(ctx, 60*time.Second)
		case "user", "remote":
			readCtx, cancelRead = context.WithTimeout(ctx, 120*time.Second)
		}
		msgType, data, err := conn.Read(readCtx)
		if cancelRead != nil {
			cancelRead()
		}
		if err != nil {
			return
		}

		// Binary messages from the remote exe are raw JPEG frames.
		if msgType == websocket.MessageBinary {
			if remoteSession != nil && clientType == "remote" {
				if !h.relayRemoteFrame(store, remoteSession, &fs, data) {
					conn.Close(websocket.StatusPolicyViolation, "session ended")
					return
				}
			}
			continue
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		// Route messages based on client type
		switch clientType {
		case "device":
			h.handleDeviceMessage(client, msg, store, deviceKey)
		case "remote", "user":
			// H1: browsers cannot initiate protocol-level pings, so the
			// web client sends an app-level ping every 30 s; answer with a
			// pong to keep the read deadline fresh on receive-only
			// dashboard connections.
			if msg.Type == "ping" {
				if pong, err := json.Marshal(Message{Type: "pong"}); err == nil {
					select {
					case client.SendCh <- pong:
					default:
						h.dropped.Add(1)
					}
				}
			}
		}
	}
}

// maxFrameBytes bounds one binary frame from the remote exe (matches
// the 10 MB limit on the HTTP frame endpoint).
const maxFrameBytes = 10 << 20

// frameState is per-connection bookkeeping for relayRemoteFrame.
type frameState struct {
	checked time.Time
}

// relayRemoteFrame forwards one JPEG frame from a remote exe to the
// session owner's browser, like POST /api/sessions/:id/frame. The
// session row is re-read at most every 2 s (not per frame) to flip
// pending -> active on the first frame and to notice that the session
// ended. It returns false when the session is no longer live and the
// connection should be closed.
func (h *Hub) relayRemoteFrame(store *store.Store, sess *models.Session, fs *frameState, jpeg []byte) bool {
	if len(jpeg) == 0 {
		return true
	}
	if time.Since(fs.checked) > 2*time.Second {
		cur, err := store.Sessions.GetByID(sess.ID)
		if err != nil || (cur.Status != "pending" && cur.Status != "active") {
			return false
		}
		if cur.Status == "pending" {
			cur.Status = "active"
			if err := store.Sessions.Update(cur); err != nil {
				log.Printf("ws: failed to mark session %s active: %v", cur.ID, err)
			}
		}
		fs.checked = time.Now()
	}
	// Only the session owner receives frames.
	if err := h.SendToUser(sess.UserID, "session_frame", map[string]interface{}{
		"session_id": sess.ID,
		"device_id":  sess.DeviceID,
		"data":       base64.StdEncoding.EncodeToString(jpeg),
	}); err != nil {
		log.Printf("ws: dropping frame for session %s: %v", sess.ID, err)
	}
	events.Publish("session_frame", map[string]interface{}{
		"session_id": sess.ID,
		"device_id":  sess.DeviceID,
	})
	return true
}

// wsCredential extracts the credential from the Sec-WebSocket-Protocol
// header (C5). Expected form: "ourway-auth, <credential>" for agents and
// browsers, or "ourway-auth, <credential>, remote" for the per-session
// remote-control executable, which connects with the device's key but
// must not supersede the agent's own connection.
func wsCredential(c *gin.Context) (string, bool) {
	raw := c.GetHeader("Sec-WebSocket-Protocol")
	if raw == "" {
		return "", false
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) >= 2 && parts[0] == "ourway-auth" {
		return parts[1], len(parts) >= 3 && parts[2] == "remote"
	}
	return "", false
}

// originAllowed reports whether a browser Origin header may open a WS
// connection (C5). When an explicit allow-list (wsOrigins) is configured,
// only those hosts are accepted; otherwise we fall back to same-origin,
// local dev origins, and the configured web URL's host.
func (h *Hub) originAllowed(origin string, c *gin.Context) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if len(h.wsOrigins) > 0 {
		for _, o := range h.wsOrigins {
			if o == u.Host {
				return true
			}
		}
		return false
	}
	if u.Host == c.Request.Host {
		return true
	}
	if u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" {
		return true
	}
	if h.webURL != "" {
		if wu, err := url.Parse(h.webURL); err == nil && wu.Host != "" && wu.Host == u.Host {
			return true
		}
	}
	return false
}

// splitOrigins parses a comma-separated origin allow-list, trimming
// whitespace and dropping empty entries.
func splitOrigins(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// clearDevicePresence removes the device's presence key, but only if this
// connection still owns it (H2).
func (h *Hub) clearDevicePresence(deviceKey, connID string) {
	if h.redis == nil || connID == "" {
		return
	}
	if h.redis.Del == nil {
		return
	}
	if h.redis.Get != nil {
		var v string
		if err := h.redis.Get(devicePresenceKey(deviceKey), &v); err != nil {
			return
		}
		if v != connID {
			return
		}
	}
	_ = h.redis.Del(devicePresenceKey(deviceKey))
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

func (h *Hub) handleDeviceMessage(client *Client, msg Message, store *store.Store, deviceKey string) {
	switch msg.Type {
	case "heartbeat":
		dev, err := store.Devices.GetByKey(deviceKey)
		if err != nil {
			return
		}
		wasOffline := dev.Status == "offline"
		// Agents report their IPs in every heartbeat; refresh the stored
		// record when they change (registration only happens at install
		// time, so this is the only path that keeps them current). This
		// runs before UpdateLastSeen: Update saves the whole stale dev
		// snapshot, so doing it after would clobber the fresh last_seen.
		if p, ok := msg.Payload.(map[string]interface{}); ok {
			changed := false
			if ip, ok := p["private_ip"].(string); ok && ip != "" && ip != dev.PrivateIP {
				dev.PrivateIP = ip
				changed = true
			}
			if ip, ok := p["public_ip"].(string); ok && ip != "" && ip != dev.PublicIP {
				dev.PublicIP = ip
				changed = true
			}
			if changed {
				if err := store.Devices.Update(dev); err != nil {
					log.Printf("ws: heartbeat IP update failed: %v", err)
				}
			}
		}
		if err := store.Devices.UpdateLastSeen(dev.ID); err != nil {
			log.Printf("ws: heartbeat update failed: %v", err)
		}
		// Fully automatic agent updates: piggyback the server version on
		// the heartbeat path so a connected agent learns within one
		// heartbeat (15 s) that a newer build is available, without a
		// separate poll. The agent compares and self-updates.
		if ack, err := json.Marshal(Message{Type: "server_version", Payload: gin.H{
			"version":    config.Version,
			"git_commit": config.GitCommit,
		}}); err == nil {
			select {
			case client.SendCh <- ack:
			default:
			}
		}
		if wasOffline {
			events.Publish("device_online", map[string]interface{}{
				"device_id": dev.ID,
				"name":      dev.Name,
			})
		}
	case "metrics":
		// C2: trust the connection's authenticated device key, not a key
		// declared in the payload (any agent could otherwise push metrics
		// for any other device).
		dev, err := store.Devices.GetByKey(deviceKey)
		if err != nil {
			return
		}
		payloadMap, ok := msg.Payload.(map[string]interface{})
		if !ok {
			return
		}
		dataVal, ok := payloadMap["data"].(map[string]interface{})
		if !ok {
			return
		}
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

		// M5: alert evaluation leaves the read path — the engine does its
		// own DB writes and a slow database must not stall heartbeats.
		select {
		case h.alertEval <- alertJob{
			metrics: models.Metrics{
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
			},
			deviceID:   dev.ID,
			deviceName: dev.Name,
		}:
		default:
			// M10: count the drop instead of silently discarding it.
			h.dropped.Add(1)
			log.Printf("ws: alert evaluation queue full, dropping evaluation for device %s", dev.ID)
		}

		// Extract and save metrics to database
		if store.MetricHistory != nil {
			if err := store.MetricHistory.Insert(mh); err != nil {
				log.Printf("ws: failed to save metrics: %v", err)
			}
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
		_ = store.Devices.UpdateLastSeen(dev.ID)
	case "status":
		h.BroadcastMessage("status", msg.Payload)
	}
}
