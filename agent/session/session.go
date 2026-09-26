package session

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"ourway/agent/collector"
)

// SessionManager manages remote control sessions for the agent.
type SessionManager struct {
	deviceKey string
	capture   ScreenCapture
	active    bool
	onSessionStart func()
	onSessionEnd   func()
}

// NewSessionManager creates a new session manager.
func NewSessionManager(deviceKey string) *SessionManager {
	return &SessionManager{
		deviceKey: deviceKey,
		capture:   NewScreenCapture(),
		active:    false,
	}
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
	case "command":
		sm.HandleCommand(ctx, payload)
	case "scan_updates":
		sm.ScanUpdates(ctx, payload)
	case "deploy_updates":
		sm.DeployUpdates(ctx, payload)
	}
}

// StartSession begins a remote control session.
func (sm *SessionManager) StartSession(ctx context.Context, payload interface{}) {
	if sm.active {
		log.Printf("session: already active, ignoring start")
		return
	}

	sessionData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("session: failed to marshal payload: %v", err)
		return
	}

	log.Printf("session: starting session: %s", string(sessionData))
	sm.active = true

	if sm.onSessionStart != nil {
		sm.onSessionStart()
	}

	// Start screen capture loop
	go sm.captureLoop(ctx)
}

// EndSession ends the current remote control session.
func (sm *SessionManager) EndSession() {
	if !sm.active {
		return
	}

	log.Printf("session: ending session")
	sm.active = false

	if sm.onSessionEnd != nil {
		sm.onSessionEnd()
	}
}

// HandleInput processes a keyboard/mouse input event.
func (sm *SessionManager) HandleInput(payload interface{}) {
	if !sm.active {
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

	switch inputType {
	case "key":
		sm.handleKeyEvent(input)
	case "mouse":
		sm.handleMouseEvent(input)
	}
}

// HandleCommand processes a command event (e.g., reboot, install).
func (sm *SessionManager) HandleCommand(ctx context.Context, payload interface{}) {
	if !sm.active {
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
			if !sm.active {
				return
			}

			frame, err := sm.capture.Capture()
			if err != nil {
				log.Printf("session: capture error: %v", err)
				continue
			}

			// In a full implementation, this would send the frame over
			// the WebRTC data channel. For now, we just log.
			_ = frame
		}
	}
}

func (sm *SessionManager) handleKeyEvent(input map[string]interface{}) {
	key, _ := input["key"].(string)
	event, _ := input["event"].(string)
	log.Printf("session: key event: %s %s", key, event)
	// TODO: Synthesize key events on the device
}

func (sm *SessionManager) handleMouseEvent(input map[string]interface{}) {
	x, _ := input["x"].(float64)
	y, _ := input["y"].(float64)
	button, _ := input["button"].(string)
	log.Printf("session: mouse event: %s at (%.0f, %.0f)", button, x, y)
	// TODO: Move mouse and click on the device
}

// ScanUpdates scans for available software updates.
func (sm *SessionManager) ScanUpdates(ctx context.Context, payload interface{}) {
	log.Printf("session: scanning for updates...")
	
	packages, err := collector.CollectSoftwarePackages()
	if err != nil {
		log.Printf("session: failed to collect packages: %v", err)
		return
	}
	
	log.Printf("session: found %d installed packages", len(packages))
	
	// In a full implementation, this would compare against a catalog
	// and report available updates to the server.
}

// DeployUpdates deploys approved software updates.
func (sm *SessionManager) DeployUpdates(ctx context.Context, payload interface{}) {
	log.Printf("session: deploying updates...")
	
	deployData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("session: failed to marshal payload: %v", err)
		return
	}
	
	log.Printf("session: deploying: %s", string(deployData))
	
	// In a full implementation, this would download and install updates.
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
	return sm.active
}
