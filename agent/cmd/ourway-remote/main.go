package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"

	"ourway/agent/config"
	"ourway/agent/session"
)

// ourway-remote is the per-session remote-control executable, the
// ScreenConnect-style split from the RMM agent: the agent service
// spawns this process in the interactive user session for each remote
// session and supervises it. It owns capture and input for the session
// and talks to the server directly, so a crash or hang here never
// takes the monitoring agent down with it.
//
// It connects with the "remote" role marker so the server routes
// session traffic (input, quality, end) to this process instead of the
// agent's connection.
func main() {
	var serverURL, deviceKey, sessionID string
	// useToken is true when deviceKey holds a per-session token (--token)
	// rather than the legacy device key (--key).
	var useToken bool
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--server", "-server":
			if i+1 < len(args) {
				serverURL = args[i+1]
			}
		case "--token", "-token", "--key", "-key":
			if i+1 < len(args) {
				deviceKey = args[i+1]
				useToken = strings.Contains(args[i], "token")
			}
		case "--session-id", "-session-id":
			if i+1 < len(args) {
				sessionID = args[i+1]
			}
		}
	}
	if serverURL == "" || deviceKey == "" || sessionID == "" {
		fmt.Fprintf(os.Stderr, "usage: ourway-remote --server URL --token TOKEN --session-id ID\n")
		os.Exit(2)
	}

	setupLogging()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	log.Printf("ourway-remote: session %s starting (server %s)", sessionID, serverURL)

	capture := session.NewScreenCapture()
	capture.SetQuality(80)
	if c, ok := capture.(interface {
		StartCapture(ctx context.Context) error
	}); ok {
		if err := c.StartCapture(ctx); err != nil {
			log.Printf("ourway-remote: capture start failed: %v", err)
		}
		defer func() {
			if c, ok := capture.(interface{ StopCapture() }); ok {
				c.StopCapture()
			}
		}()
	}

	// Connect with the remote role marker. nhooyr.io/websocket accepts
	// plain http(s) URLs for Dial.
	wsURL := config.HTTPBaseURL(serverURL) + "/ws"
	var conn *websocket.Conn
	deadline := time.Now().Add(30 * time.Second)
	var err error
	for time.Now().Before(deadline) {
		conn, _, err = websocket.Dial(ctx, wsURL, &websocket.DialOptions{
			Subprotocols: []string{"ourway-auth", deviceKey, "remote"},
		})
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "but got 401") {
			log.Fatalf("ourway-remote: server rejected device key (HTTP 401)")
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		log.Fatalf("ourway-remote: cannot connect: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "session done")
	log.Printf("ourway-remote: connected as remote for session %s", sessionID)

	// Session start/end arrive over this connection; input too. Read
	// in a goroutine, run the capture loop on the main goroutine.
	var ended atomic.Bool
	// binaryOK is set once the server announces it accepts binary frames.
	var binaryOK atomic.Bool
	// H1 parity with the browser client: the server enforces a 120 s
	// read deadline on remote connections and browsers cannot initiate
	// protocol-level pings, so an app-level ping every 30 s keeps the
	// deadline fresh. nhooyr answers the server's protocol pings
	// automatically, but the read deadline needs inbound traffic too.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		ping, _ := json.Marshal(struct {
			Type string `json:"type"`
		}{"ping"})
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(wctx, websocket.MessageText, ping)
				wcancel()
				if err != nil {
					ended.Store(true)
					return
				}
			}
		}
	}()
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				ended.Store(true)
				return
			}
			var msg struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "binary_frames":
				binaryOK.Store(true)
				log.Printf("ourway-remote: server accepts binary frames")
			case "session_end":
				ended.Store(true)
				return
			case "session_quality":
				var q struct {
					Quality int `json:"quality"`
				}
				if json.Unmarshal(msg.Payload, &q) == nil {
					capture.SetQuality(q.Quality)
				}
			case "input":
				var in struct {
					Type  string          `json:"type"`
					Value json.RawMessage `json:"payload"`
				}
				if json.Unmarshal(msg.Payload, &in) != nil {
					continue
				}
				var m map[string]interface{}
				if json.Unmarshal(in.Value, &m) != nil {
					continue
				}
				switch in.Type {
				case "key":
					key, _ := m["key"].(string)
					event, _ := m["event"].(string)
					session.InjectKey(key, event)
				case "mouse":
					event, _ := m["event"].(string)
					x, _ := m["x"].(float64)
					y, _ := m["y"].(float64)
					button, _ := m["button"].(string)
					delta, _ := m["delta"].(float64)
					session.InjectMouse(event, x, y, button, delta)
				}
			}
		}
	}()

	loggedFirst := false
	frameURL := config.HTTPBaseURL(serverURL) + "/api/sessions/" + sessionID + "/frame"
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// Capture loop: ~15fps like the agent's session manager.
	interval := time.Second / 15
	for !ended.Load() {
		select {
		case <-ctx.Done():
		default:
		}
		frame, err := capture.Capture()
		if err != nil {
			log.Printf("ourway-remote: capture error: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if useToken && binaryOK.Load() {
			// Frames ride the already-open WebSocket as binary messages:
			// no per-frame HTTP request or TLS/connection churn.
			wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
			werr := conn.Write(wctx, websocket.MessageBinary, frame)
			wcancel()
			if werr != nil {
				log.Printf("ourway-remote: frame write failed: %v; ending", werr)
				return
			}
			if !loggedFirst {
				loggedFirst = true
				log.Printf("ourway-remote: first frame sent over websocket (%d bytes)", len(frame))
			}
			time.Sleep(interval)
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "POST", frameURL, bytes.NewReader(frame))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "image/jpeg")
		if useToken {
			req.Header.Set("X-Session-Token", deviceKey)
		} else {
			req.Header.Set("X-Device-Key", deviceKey)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			log.Printf("ourway-remote: frame upload failed: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		resp.Body.Close()
		if !loggedFirst && resp.StatusCode < 300 {
			loggedFirst = true
			log.Printf("ourway-remote: first frame sent over HTTP (%d bytes)", len(frame))
		}
		if resp.StatusCode >= 300 {
			log.Printf("ourway-remote: frame upload returned %d; ending", resp.StatusCode)
			return
		}
		time.Sleep(interval)
	}
	log.Printf("ourway-remote: session %s ended", sessionID)
}
