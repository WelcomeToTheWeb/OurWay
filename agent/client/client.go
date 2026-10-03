package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"nhooyr.io/websocket"
	"os"
	"strings"
	"sync"
	"time"

	"ourway/agent/config"
	"ourway/agent/files"
	"ourway/agent/patch"
	"ourway/agent/session"
)

// Client connects to the OurWay server via WebSocket and sends metrics.
type Client struct {
	serverURL    string
	deviceKey    string
	deviceID     string
	heartbeatSec time.Duration
	metricsSec   time.Duration
	streamSec    time.Duration
	metricsFunc  func() (interface{}, error)
	sessionMgr   *session.SessionManager
	fileHandler  *files.Handler
	patchHandler *patch.Handler

	// publicIP is resolved once at startup (best-effort); the private IP
	// is re-evaluated per heartbeat so the server's record tracks network
	// changes. Both are reported in every heartbeat frame so the server
	// can keep the device's public/private IP fields current — the
	// registration request only happens once, at install time.
	publicIP   string
	publicOnce sync.Once
}

// Option is a function that configures the client.
type Option func(*Client)

// WithHeartbeatInterval sets the heartbeat interval.
func WithHeartbeatInterval(d time.Duration) Option {
	return func(c *Client) {
		c.heartbeatSec = d
	}
}

// WithMetricsInterval sets the metrics collection interval.
func WithMetricsInterval(d time.Duration) Option {
	return func(c *Client) {
		c.metricsSec = d
	}
}

// WithStreamInterval sets the streaming interval when server requests streaming.
func WithStreamInterval(d time.Duration) Option {
	return func(c *Client) {
		c.streamSec = d
	}
}

// WithMetricsFunc sets the function used to collect metrics.
func WithMetricsFunc(f func() (interface{}, error)) Option {
	return func(c *Client) {
		c.metricsFunc = f
	}
}

// WithDeviceID sets this agent's own device ID, used by the patch handler
// to verify that server payloads are addressed to this device.
func WithDeviceID(id string) Option {
	return func(c *Client) {
		c.deviceID = id
	}
}

// New creates a new WebSocket client. The device ID (used to verify
// inbound patch payloads) must be set with WithDeviceID before the first
// message is processed.
func New(serverURL, deviceKey string, opts ...Option) *Client {
	c := &Client{
		serverURL:    serverURL,
		deviceKey:    deviceKey,
		heartbeatSec: 15 * time.Second,
		metricsSec:   60 * time.Second,
		streamSec:    2 * time.Second,
		sessionMgr:   session.NewSessionManager(deviceKey),
		fileHandler:  files.NewHandler(deviceKey, config.HTTPBaseURL(serverURL)),
	}
	for _, opt := range opts {
		opt(c)
	}
	c.patchHandler = patch.NewHandler(deviceKey, config.HTTPBaseURL(serverURL), c.deviceID)
	if serverURL != "" {
		// The agent's own server URL is authoritative for frame uploads:
		// it is the endpoint the WebSocket already reaches, so it always
		// carries the right scheme/host/port (unlike the server's
		// browser-derived session_start URL).
		c.sessionMgr.SetServerURL(config.HTTPBaseURL(serverURL))
	}
	return c
}

// ResolveDeviceID fetches this device's ID from the server using the
// device key (GET /api/agent/me).
func ResolveDeviceID(baseURL, deviceKey string) (string, error) {
	req, err := http.NewRequest("GET", baseURL+"/api/agent/me", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Device-Key", deviceKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d for /api/agent/me", resp.StatusCode)
	}

	var out struct {
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Device.ID == "" {
		return "", fmt.Errorf("server response missing device id")
	}
	return out.Device.ID, nil
}

// Run starts the client and blocks until the context is cancelled.
func (c *Client) Run(ctx context.Context) error {
	// Resolve the public IP once, best-effort, before the first
	// heartbeat so the server's record gets it as early as possible;
	// then refresh it in the background (M4: a NAT reassignment after
	// startup would otherwise stay stale while the private IP keeps
	// refreshing every heartbeat).
	c.publicOnce.Do(func() {
		pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		c.publicIP = FetchPublicIP(pubCtx)
	})
	go c.refreshPublicIP(ctx)
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		err := c.connect(ctx)
		if err != nil {
			log.Printf("connection error: %v, retrying in %v", err, backoff)
			time.Sleep(backoff)
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			continue
		}

		// Connection successful, reset backoff
		backoff = time.Second
	}
}

// refreshPublicIP re-resolves the public IP every 10 minutes so a
// network change shows up in heartbeats; failures keep the previous
// value (best effort).
func (c *Client) refreshPublicIP(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if ip := FetchPublicIP(pubCtx); ip != "" {
				c.publicIP = ip
			}
			cancel()
		}
	}
}

// readResult carries one inbound WebSocket read from the background reader.
type readResult struct {
	msgType websocket.MessageType
	data    []byte
	err     error
}

func (c *Client) connect(ctx context.Context) error {
	// Ensure the URL has a path
	serverURL := c.serverURL
	if serverURL == "" {
		serverURL = "ws://localhost:8080"
	}
	u, err := url.Parse(serverURL)
	if err != nil {
		return err
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	log.Printf("connecting to %s", u.String())

	// C5: the device key travels in the Sec-WebSocket-Protocol list
	// ("ourway-auth, <key>"), not in the query string — query strings
	// end up in access logs and proxy logs.
	conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPClient:   http.DefaultClient,
		Subprotocols: []string{"ourway-auth", c.deviceKey},
	})
	if err != nil {
		// nhooyr.io/websocket returns an untyped error for non-101
		// handshake responses; detect 401 by its message so an
		// unregistered device key gets an actionable log line instead
		// of an opaque retry loop.
		if strings.Contains(err.Error(), "but got 401") {
			return fmt.Errorf("server rejected device key (HTTP 401): this device is not registered with the server; re-run the installer on this machine")
		}
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "closing")

	log.Printf("connected to server as %s", c.deviceKey)

	// Single-writer pattern: nhooyr.io/websocket allows one reader and one
	// writer only. Every outbound frame (heartbeat, metrics) is enqueued
	// on sendCh and written by this one goroutine, so writes can never
	// interleave and corrupt the connection.
	sendCh := make(chan []byte, 16)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-sendCh:
				if !ok {
					return
				}
				if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
					log.Printf("websocket write: %v", err)
					// The connection is broken; stop writing. The read
					// loop will surface the error and tear down the
					// connection.
					return
				}
			}
		}
	}()
	// Close sendCh last so the writer goroutine always exits (no leak)
	// when connect() returns.
	defer func() {
		close(sendCh)
		<-writerDone
	}()

	// enqueue marshals msg as JSON and queues it for the writer goroutine.
	// If the queue is full the frame is dropped with a log line so the
	// callers (heartbeat loop, metrics) never block.
	enqueue := func(msg interface{}) {
		data, err := json.Marshal(msg)
		if err != nil {
			log.Printf("marshal outbound message: %v", err)
			return
		}
		select {
		case sendCh <- data:
		default:
			log.Printf("websocket send queue full; dropping frame")
		}
	}

	// Set up timers
	heartbeatTimer := time.NewTimer(c.heartbeatSec)
	defer heartbeatTimer.Stop()
	metricsTimer := time.NewTimer(c.metricsSec)
	defer metricsTimer.Stop()

	// Send initial metrics immediately if we have a metrics func
	if c.metricsFunc != nil {
		go c.sendMetrics(ctx, sendCh)
	}

	// Track streaming mode
	currentInterval := c.metricsSec

	// Read incoming messages in a goroutine so the select below can always
	// service the heartbeat and metrics timers; a blocking conn.Read in the
	// select's default branch would starve them.
	readCh := make(chan readResult, 16)
	go func() {
		for {
			msgType, data, err := conn.Read(ctx)
			select {
			case readCh <- readResult{msgType: msgType, data: data, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-heartbeatTimer.C:
			enqueue(map[string]interface{}{
				"type": "heartbeat",
				"payload": map[string]interface{}{
					"device_key": c.deviceKey,
					"timestamp":  time.Now().Unix(),
					// Keep the server's IP fields current: registration
					// only happens once at install time.
					"private_ip": PrivateIP(),
					"public_ip":  c.publicIP,
				},
			})
			heartbeatTimer.Reset(c.heartbeatSec)

		case <-metricsTimer.C:
			if c.metricsFunc != nil {
				go c.sendMetrics(ctx, sendCh)
			}
			metricsTimer.Reset(currentInterval)

		case r := <-readCh:
			if r.err != nil {
				return fmt.Errorf("read message: %w", r.err)
			}

			if r.msgType == websocket.MessageText {
				var msgData map[string]interface{}
				if err := json.Unmarshal(r.data, &msgData); err == nil {
					msgTypeName, _ := msgData["type"].(string)

					switch msgTypeName {
					case "stream":
						// The server wraps the interval in the payload;
						// clamp to 1..3600 s so a 0/negative value can't
						// spin the collection loop (L3).
						interval := 2
						if payload, ok := msgData["payload"].(map[string]interface{}); ok {
							if v, ok := payload["interval"].(float64); ok {
								interval = int(v)
							}
						}
						if interval < 1 || interval > 3600 {
							interval = 2
						}
						currentInterval = time.Duration(interval) * time.Second
						log.Printf("streaming mode: %d second interval", interval)
						metricsTimer.Reset(currentInterval)
					case "stream_end":
						currentInterval = c.metricsSec
						log.Printf("streaming ended, reverting to %v interval", currentInterval)
						metricsTimer.Reset(currentInterval)
					case "session_start", "session_end", "session_quality", "input", "command":
						c.sessionMgr.HandleMessage(ctx, msgTypeName, msgData["payload"])
					case "file_push":
						go c.fileHandler.HandlePush(ctx, msgData["payload"])
					case "file_pull":
						go c.fileHandler.HandlePull(ctx, msgData["payload"])
					case "scan_updates":
						go c.patchHandler.ScanUpdates(ctx, msgData["payload"])
					case "deploy_updates":
						go c.patchHandler.DeployUpdates(ctx, msgData["payload"])
					case "rollback_updates":
						go c.patchHandler.RollbackUpdates(ctx, msgData["payload"])
					case "reboot":
						go c.patchHandler.Reboot(msgData["payload"])
					}
				}
			}
		}
	}
}

func (c *Client) sendMetrics(ctx context.Context, sendCh chan []byte) {
	if c.metricsFunc == nil {
		return
	}

	data, err := c.metricsFunc()
	if err != nil {
		log.Printf("error collecting metrics: %v", err)
		return
	}

	hostname, _ := os.Hostname()

	msg := map[string]interface{}{
		"type": "metrics",
		"payload": map[string]interface{}{
			"device_key": c.deviceKey,
			"hostname":   hostname,
			"timestamp":  time.Now().Unix(),
			"data":       data,
		},
	}

	encoded, err := json.Marshal(msg)
	if err != nil {
		log.Printf("error sending metrics: %v", err)
		return
	}

	select {
	case sendCh <- encoded:
	default:
		log.Printf("websocket send queue full; dropping metrics frame")
	}
}
