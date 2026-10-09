package session

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxSessionDuration bounds how long a session may run locally. The server
// is the primary authority (session_end), but if it dies mid-session the
// agent must not keep capturing and uploading frames forever.
const maxSessionDuration = 24 * time.Hour

// sessionExtras is implemented by platform captures that need
// per-session setup (Windows: the frame producer, which refuses to run
// in the service's Session 0).
type sessionExtras interface {
	startSession(ctx context.Context) error
	stopSession()
}

type SessionManager struct {
	deviceKey string
	capture   ScreenCapture

	// mu guards active, sessionID, serverURL and startedAt: they are
	// written by the WS message goroutine and read by the capture loop.
	mu        sync.Mutex
	active    bool
	sessionID string
	// remoteToken is the per-session credential for the remote-control
	// exe (sent in session_start); the device key stays in the agent.
	remoteToken string
	serverURL   string
	startedAt   time.Time

	httpClient     *http.Client
	onSessionStart func()
	onSessionEnd   func()
}

// NewSessionManager creates a new session manager.
func NewSessionManager(deviceKey string) *SessionManager {
	return &SessionManager{
		deviceKey:  deviceKey,
		capture:    NewScreenCapture(),
		active:     false,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// sessionRemoteToken returns the current session's remote-exe token.
func (sm *SessionManager) sessionRemoteToken() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.remoteToken
}

// SetServerURL configures the server base URL used to upload frames.
// The server also sends it in the "session_start" payload; this setter
// exists so the client can pre-configure it if needed.
func (sm *SessionManager) SetServerURL(url string) {
	sm.mu.Lock()
	sm.serverURL = strings.TrimSuffix(url, "/")
	sm.mu.Unlock()
}

// stateSnapshot returns a consistent view of the mutable session state.
func (sm *SessionManager) stateSnapshot() (active bool, sessionID, serverURL string, startedAt time.Time) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.active, sm.sessionID, sm.serverURL, sm.startedAt
}

// HandleMessage processes a session-related message from the server.
func (sm *SessionManager) HandleMessage(ctx context.Context, msgType string, payload interface{}) {
	switch msgType {
	case "session_start":
		sm.StartSession(ctx, payload)
	case "session_end":
		sm.EndSession()
	case "input":
		sm.HandleInput(payload)
	case "session_quality":
		sm.HandleQuality(payload)
	case "command":
		sm.HandleCommand(ctx, payload)
	case "send_sas":
		sm.HandleSendSAS()
	}
	// scan_updates / deploy_updates are intentionally NOT handled here:
	// the client routes them to the patch handler, which is the real
	// implementation.
}

// HandleSendSAS raises Ctrl+Alt+Del for the technician's viewer. Only
// this service (SYSTEM) may do so; the user-session exe cannot.
func (sm *SessionManager) HandleSendSAS() {
	if !sm.IsActive() {
		log.Printf("session: send_sas ignored: no active session")
		return
	}
	if err := SendSAS(); err != nil {
		log.Printf("session: send_sas failed: %v", err)
		return
	}
	log.Printf("session: Ctrl+Alt+Del sent")
}

// StartSession begins a remote control session.
func (sm *SessionManager) StartSession(ctx context.Context, payload interface{}) {
	sm.mu.Lock()
	if sm.active {
		sm.mu.Unlock()
		log.Printf("session: already active, ignoring start")
		return
	}
	sm.active = true
	sm.startedAt = time.Now()
	sm.mu.Unlock()

	sessionData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("session: failed to marshal payload: %v", err)
		return
	}

	log.Printf("session: starting session: %s", string(sessionData))

	// Learn the session ID from the start payload so the capture loop
	// can upload frames to POST /api/sessions/:id/frame. The server base
	// URL is deliberately NOT taken from the payload: the server derives
	// it from the browser's Host header, which can omit the port or
	// point at a reverse proxy the agent cannot reach. The agent uses
	// its own configured URL (set via SetServerURL) — the same endpoint
	// it is already talking to over WebSocket.
	if m, ok := payload.(map[string]interface{}); ok {
		sm.mu.Lock()
		if id, ok := m["session_id"].(string); ok && id != "" {
			sm.sessionID = id
		}
		sm.remoteToken, _ = m["remote_token"].(string)
		sm.mu.Unlock()
	}

	if sm.onSessionStart != nil {
		sm.onSessionStart()
	}

	// Windows: prefer the per-session remote-control executable (the
	// ScreenConnect-style split). It runs in the interactive user
	// session and owns capture, input and frame upload; the agent only
	// spawns and supervises it. Where the split does not apply (interactive
	// runs, unsupported platforms) or the exe cannot start, the in-process
	// capture below runs instead.
	if startRemoteSession(sm) {
		return
	}
	// Platform session setup (Windows: the frame producer; it fails in
	// Session 0 when the remote-control exe could not be started).
	if ex, ok := sm.capture.(sessionExtras); ok {
		if err := ex.startSession(ctx); err != nil {
			log.Printf("session: platform session setup failed: %v; ending session", err)
			sm.EndSession()
			return
		}
	}

	// Start screen capture loop
	go sm.captureLoop(ctx)
}

// EndSession ends the current remote control session.
func (sm *SessionManager) EndSession() {
	sm.mu.Lock()
	if !sm.active {
		sm.mu.Unlock()
		return
	}
	sm.active = false
	sm.mu.Unlock()

	log.Printf("session: ending session")
	stopRemoteSession()

	if ex, ok := sm.capture.(sessionExtras); ok {
		ex.stopSession()
	}

	if sm.onSessionEnd != nil {
		sm.onSessionEnd()
	}
}

// HandleInput processes a keyboard/mouse input event.
func (sm *SessionManager) HandleInput(payload interface{}) {
	active, _, _, _ := sm.stateSnapshot()
	if !active {
		return
	}

	inputData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("session: failed to marshal input payload: %v", err)
		return
	}

	var input map[string]interface{}
	if err := json.Unmarshal(inputData, &input); err != nil {
		log.Printf("session: failed to parse input: %v", err)
		return
	}

	inputType, _ := input["type"].(string)
	// The server forwards the web client's nested shape:
	// {"type": "key"|"mouse", "payload": {event fields}} - the event
	// fields live under "payload", not at the top level.
	event, _ := input["payload"].(map[string]interface{})

	switch inputType {
	case "key":
		sm.handleKeyEvent(event)
	case "mouse":
		sm.handleMouseEvent(event)
	}
}

// HandleQuality updates the screen-capture JPEG quality (0-100).
func (sm *SessionManager) HandleQuality(payload interface{}) {
	if m, ok := payload.(map[string]interface{}); ok {
		if q, ok := m["quality"].(float64); ok {
			sm.capture.SetQuality(int(q))
			log.Printf("session: capture quality set to %d", int(q))
		}
	}
}

// HandleCommand processes a command event (e.g., reboot, install).
func (sm *SessionManager) HandleCommand(ctx context.Context, payload interface{}) {
	active, _, _, _ := sm.stateSnapshot()
	if !active {
		return
	}

	cmdData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("session: failed to marshal command payload: %v", err)
		return
	}

	log.Printf("session: received command: %s", string(cmdData))
}

// captureLoop continuously captures the screen and sends frames.
func (sm *SessionManager) captureLoop(ctx context.Context) {
	fps := 15
	interval := time.Second / time.Duration(fps)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			active, _, _, startedAt := sm.stateSnapshot()
			if !active {
				return
			}
			// Local stop timeout: the server should end the session, but if
			// it is gone the agent must not capture/upload forever.
			if time.Since(startedAt) > maxSessionDuration {
				log.Printf("session: max session duration (%s) reached, stopping", maxSessionDuration)
				sm.EndSession()
				return
			}

			frame, err := sm.capture.Capture()
			if err != nil {
				log.Printf("session: capture error: %v", err)
				continue
			}

			// Reliable path: upload the JPEG to the server, which relays it
			// to the browser over the user WebSocket.
			sm.postFrame(frame)
		}
	}
}

// postFrame uploads a JPEG frame to the server's frame endpoint.
func (sm *SessionManager) postFrame(frame []byte) {
	_, sessionID, serverURL, _ := sm.stateSnapshot()
	if serverURL == "" {
		log.Printf("session: server URL not set; frame dropped (cannot reach POST /api/sessions/:id/frame)")
		return
	}
	if sessionID == "" {
		log.Printf("session: no session ID; frame dropped")
		return
	}

	url := serverURL + "/api/sessions/" + sessionID + "/frame"
	req, err := http.NewRequest("POST", url, bytes.NewReader(frame))
	if err != nil {
		log.Printf("session: failed to build frame request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("X-Device-Key", sm.deviceKey)

	resp, err := sm.httpClient.Do(req)
	if err != nil {
		log.Printf("session: frame upload failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("session: frame upload returned %d", resp.StatusCode)
	}
}

func (sm *SessionManager) handleKeyEvent(input map[string]interface{}) {
	key, _ := input["key"].(string)
	event, _ := input["event"].(string)
	if key == "" {
		return
	}
	log.Printf("session: key event: %s %s", event, key)
	synthesizeKey(key, event)
}

func (sm *SessionManager) handleMouseEvent(input map[string]interface{}) {
	event, _ := input["event"].(string)
	x, _ := input["x"].(float64)
	y, _ := input["y"].(float64)
	button, _ := input["button"].(string)
	delta, _ := input["delta"].(float64)
	log.Printf("session: mouse event: %s at (%.0f, %.0f) button=%s", event, x, y, button)
	synthesizeMouse(event, x, y, button, delta)
}

// SetOnSessionStart registers a callback for when a session starts.
func (sm *SessionManager) SetOnSessionStart(fn func()) {
	sm.onSessionStart = fn
}

// SetOnSessionEnd registers a callback for when a session ends.
func (sm *SessionManager) SetOnSessionEnd(fn func()) {
	sm.onSessionEnd = fn
}

// IsActive returns whether a session is currently active.
func (sm *SessionManager) IsActive() bool {
	active, _, _, _ := sm.stateSnapshot()
	return active
}
