package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"ourway/agent/config"
	"ourway/agent/files"
	"ourway/agent/patch"
	"ourway/agent/session"
)

// Client connects to the OurWay server via WebSocket and sends metrics.
type Client struct {
	serverURL    string
	deviceKey    string
	heartbeatSec time.Duration
	metricsSec   time.Duration
	streamSec    time.Duration
	metricsFunc  func() (interface{}, error)
	sessionMgr   *session.SessionManager
	fileHandler  *files.Handler
	patchHandler *patch.Handler
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

// New creates a new WebSocket client.
func New(serverURL, deviceKey string, opts ...Option) *Client {
	c := &Client{
		serverURL:    serverURL,
		deviceKey:    deviceKey,
		heartbeatSec: 15 * time.Second,
		metricsSec:   60 * time.Second,
		streamSec:    2 * time.Second,
		sessionMgr:   session.NewSessionManager(deviceKey),
		fileHandler:  files.NewHandler(deviceKey, config.HTTPBaseURL(serverURL)),
		patchHandler: patch.NewHandler(deviceKey, config.HTTPBaseURL(serverURL)),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Run starts the client and blocks until the context is cancelled.
func (c *Client) Run(ctx context.Context) error {
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
		serverURL = "ws://localhost:8081"
	}
	u, err := url.Parse(serverURL)
	if err != nil {
		return err
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	log.Printf("connecting to %s", u.String())
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("device_key", c.deviceKey)
	u.RawQuery = q.Encode()

	conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPClient: http.DefaultClient,
	})
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "closing")

	log.Printf("connected to server as %s", c.deviceKey)

	// Set up timers
	heartbeatTimer := time.NewTimer(c.heartbeatSec)
	defer heartbeatTimer.Stop()
	metricsTimer := time.NewTimer(c.metricsSec)
	defer metricsTimer.Stop()

	// Send initial metrics immediately if we have a metrics func
	if c.metricsFunc != nil {
		go c.sendMetrics(ctx, conn)
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
			msg := map[string]interface{}{
				"type": "heartbeat",
				"payload": map[string]interface{}{
					"device_key": c.deviceKey,
					"timestamp":  time.Now().Unix(),
				},
			}
			if err := wsjson.Write(ctx, conn, msg); err != nil {
				return fmt.Errorf("send heartbeat: %w", err)
			}
			heartbeatTimer.Reset(c.heartbeatSec)

		case <-metricsTimer.C:
			if c.metricsFunc != nil {
				go c.sendMetrics(ctx, conn)
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
						interval := 2
						if v, ok := msgData["interval"].(float64); ok {
							interval = int(v)
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

func (c *Client) sendMetrics(ctx context.Context, conn *websocket.Conn) {
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

	if err := wsjson.Write(ctx, conn, msg); err != nil {
		log.Printf("error sending metrics: %v", err)
	}
}
