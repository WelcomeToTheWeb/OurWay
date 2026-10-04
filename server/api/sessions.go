package api

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/events"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// SessionHandler handles session-related API endpoints.
type SessionHandler struct {
	store *store.Store
	hub   *ws.Hub
}

// NewSessionHandler creates a new session handler.
func NewSessionHandler(store *store.Store, hub *ws.Hub) *SessionHandler {
	return &SessionHandler{
		store: store,
		hub:   hub,
	}
}

// StartSession starts a new remote session with a device.
//
// The session is created "pending" and flips to "active" when the agent
// delivers its first frame (ReportFrame). There is no WebRTC handshake:
// frames travel over HTTP and input over the device WebSocket, so the
// browser can render the screen as soon as the first frame arrives.
//
// POST /api/devices/:id/sessions
func (h *SessionHandler) StartSession(c *gin.Context) {
	deviceID := c.Param("id")

	// Verify device exists
	device, err := h.store.Devices.GetByID(deviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"error": "device not found"})
		} else {
			c.JSON(500, gin.H{"error": "internal server error"})
		}
		return
	}

	// Check if device is connected
	if !h.hub.IsDeviceConnected(device.DeviceKey) {
		c.JSON(409, gin.H{"error": "device is not connected"})
		return
	}

	// Get current user
	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	// Per-session token for the remote-control exe: it authenticates
	// that exe for this session only, so the device key stays with the
	// agent service. Only the hash is stored.
	remoteToken, remoteHash, err := models.NewRemoteToken()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to create session"})
		return
	}

	// Token for the native viewer (ourway:// launch). Same scheme and
	// scope as the remote token, but a distinct secret.
	viewerToken, viewerHash, err := models.NewRemoteToken()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to create session"})
		return
	}

	// Create session
	session := &models.Session{
		ID:              uuid.NewString(),
		DeviceID:        deviceID,
		UserID:          userID,
		Status:          "pending",
		RemoteTokenHash: remoteHash,
		ViewerTokenHash: viewerHash,
	}
	if err := h.store.Sessions.Create(session); err != nil {
		c.JSON(500, gin.H{"error": "failed to create session"})
		return
	}

	// Tell the device to start capturing: the agent's capture loop only
	// runs after it receives "session_start" on its WS connection. The
	// server URL is included so the agent knows where to upload frames.
	serverURL := publicServerURL(c)
	if err := h.hub.SendToDevice(device.DeviceKey, "session_start", gin.H{
		"session_id":   session.ID,
		"server_url":   serverURL,
		"remote_token": remoteToken,
	}); err != nil {
		log.Printf("sessions: failed to notify device of session start: %v", err)
	}

	events.Publish("session_started", map[string]interface{}{
		"session_id": session.ID,
		"device_id":  deviceID,
		"device":     device.Name,
		"user_id":    userID,
	})

	// The viewer launch URL carries the viewer token; the server URL is
	// what the viewer dials. The token is returned only here, once.
	q := url.Values{}
	q.Set("server", serverURL)
	q.Set("token", viewerToken)
	q.Set("device", device.Name)
	c.JSON(200, gin.H{
		"session":      session,
		"viewer_token": viewerToken,
		"viewer_url":   "ourway://session/" + session.ID + "?" + q.Encode(),
	})
}

// publicServerURL is the base URL remote parties (agent, native viewer)
// should dial to reach this server. Behind a reverse proxy the request's
// own Host/TLS describe the hop from the proxy, so prefer the forwarded
// headers; the proxy must preserve the port (nginx: $http_host, not $host).
func publicServerURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if p := c.GetHeader("X-Forwarded-Proto"); p == "http" || p == "https" {
		scheme = p
	}
	host := c.Request.Host
	if h := c.GetHeader("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

// authorizeSessionAccess verifies the session exists and belongs to the
// caller. Returns the session on success; on failure it writes the
// appropriate 403/404 response and returns false.
func (h *SessionHandler) authorizeSessionAccess(c *gin.Context, sessionID string) (*models.Session, bool) {
	session, err := h.store.Sessions.GetByID(sessionID)
	if err != nil {
		c.JSON(404, gin.H{"error": "session not found"})
		return nil, false
	}

	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)
	if session.UserID != userID {
		c.JSON(403, gin.H{"error": "not authorized for this session"})
		return nil, false
	}

	return session, true
}

// EndSession ends an active session.
// DELETE /api/sessions/:id
func (h *SessionHandler) EndSession(c *gin.Context) {
	sessionID := c.Param("id")

	session, ok := h.authorizeSessionAccess(c, sessionID)
	if !ok {
		return
	}

	if err := h.store.Sessions.EndSession(sessionID); err != nil {
		c.JSON(500, gin.H{"error": "failed to end session"})
		return
	}

	// Tell the device to stop capturing.
	if device, err := h.store.Devices.GetByID(session.DeviceID); err == nil {
		if err := h.hub.SendToDevice(device.DeviceKey, "session_end", gin.H{
			"session_id": sessionID,
		}); err != nil {
			log.Printf("sessions: failed to notify device of session end: %v", err)
		}
	}

	h.hub.SendToViewer(sessionID, "session_end", gin.H{"session_id": sessionID})

	c.JSON(200, gin.H{"status": "ended"})
}

// SendInput sends keyboard/mouse input to the remote device.
// POST /api/sessions/:id/input
func (h *SessionHandler) SendInput(c *gin.Context) {
	sessionID := c.Param("id")

	var req struct {
		Type    string      `json:"type" binding:"required"` // "key", "mouse"
		Payload interface{} `json:"payload" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	// Get session (and verify ownership) to find device
	session, ok := h.authorizeSessionAccess(c, sessionID)
	if !ok {
		return
	}

	// Get device to find device key
	device, err := h.store.Devices.GetByID(session.DeviceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "device not found"})
		return
	}

	// Send input to device via WebSocket
	inputMsg := gin.H{
		"type":    req.Type,
		"payload": req.Payload,
	}

	if err := h.hub.SendToDevice(device.DeviceKey, "input", inputMsg); err != nil {
		c.JSON(409, gin.H{"error": "failed to send input to device"})
		return
	}

	c.JSON(200, gin.H{"status": "sent"})
}

// ReportFrame receives a screen frame (raw JPEG body) from the agent and
// relays it to the session owner's browser connection. This is the frame
// path for remote sessions.
// POST /api/sessions/:id/frame (device key auth via X-Device-Key, like /api/agent/*)
func (h *SessionHandler) ReportFrame(c *gin.Context) {
	sessionID := c.Param("id")
	session, err := h.store.Sessions.GetByID(sessionID)
	if err != nil {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}

	// The remote-control exe authenticates with its per-session token;
	// the agent (legacy in-process path) with the device key.
	if tok := c.GetHeader("X-Session-Token"); tok != "" {
		want := models.HashRemoteToken(tok)
		if session.RemoteTokenHash == "" || subtle.ConstantTimeCompare([]byte(want), []byte(session.RemoteTokenHash)) != 1 {
			c.JSON(401, gin.H{"error": "invalid session token"})
			return
		}
	} else {
		deviceKey := c.GetHeader("X-Device-Key")
		if deviceKey == "" {
			c.JSON(401, gin.H{"error": "missing X-Session-Token or X-Device-Key header"})
			return
		}
		device, err := h.store.Devices.GetByKey(deviceKey)
		if err != nil {
			c.JSON(401, gin.H{"error": "unknown device key"})
			return
		}
		if session.DeviceID != device.ID {
			c.JSON(403, gin.H{"error": "session does not belong to this device"})
			return
		}
	}

	// M2: reject frames for sessions that are no longer live so a
	// late/looping agent can't stream (or burn bandwidth on) an ended
	// session, and the first frame flips pending -> active.
	if session.Status == "pending" {
		session.Status = "active"
		if err := h.store.Sessions.Update(session); err != nil {
			log.Printf("sessions: failed to mark session %s active: %v", sessionID, err)
		}
	} else if session.Status != "active" {
		c.JSON(409, gin.H{"error": "session is not active"})
		return
	}

	// Raw JPEG body, bounded to 10 MB.
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 10<<20))
	if err != nil || len(data) == 0 {
		c.JSON(400, gin.H{"error": "empty frame body"})
		return
	}

	// Relay only to the session owner's browser connection so no other
	// authenticated client can watch a live session.
	frameMsg := gin.H{
		"session_id": sessionID,
		"device_id":  session.DeviceID,
		"data":       base64.StdEncoding.EncodeToString(data),
	}
	if err := h.hub.SendToUser(session.UserID, "session_frame", frameMsg); err != nil {
		// Owner not connected (or on another instance without Redis): drop
		// the frame rather than leaking it to other users.
		log.Printf("sessions: dropping frame for session %s: %v", sessionID, err)
	}
	events.Publish("session_frame", gin.H{
		"session_id": sessionID,
		"device_id":  session.DeviceID,
	})

	c.JSON(200, gin.H{"status": "ok"})
}

// SetQuality updates the screen-capture JPEG quality for a session and
// notifies the device so the capture loop honors it.
// POST /api/sessions/:id/quality
func (h *SessionHandler) SetQuality(c *gin.Context) {
	sessionID := c.Param("id")

	var req struct {
		Quality int `json:"quality" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}
	if req.Quality < 1 || req.Quality > 100 {
		c.JSON(400, gin.H{"error": "quality must be between 1 and 100"})
		return
	}

	session, ok := h.authorizeSessionAccess(c, sessionID)
	if !ok {
		return
	}
	device, err := h.store.Devices.GetByID(session.DeviceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "device not found"})
		return
	}

	if err := h.hub.SendToDevice(device.DeviceKey, "session_quality", gin.H{"quality": req.Quality}); err != nil {
		c.JSON(409, gin.H{"error": "failed to notify device"})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

// ListSessions returns live sessions. Non-admins see only their own
// sessions; admins see every pending and active session fleet-wide (M6).
// GET /api/sessions
func (h *SessionHandler) ListSessions(c *gin.Context) {
	userIDVal, _ := c.Get("user_id")
	userID, _ := userIDVal.(string)

	var (
		sessions []models.Session
		err      error
	)
	if hasRole(c, "admin") {
		sessions, err = h.store.Sessions.ListActiveAll()
	} else {
		sessions, err = h.store.Sessions.ListActiveByUser(userID)
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list sessions"})
		return
	}
	c.JSON(200, gin.H{"sessions": sessions})
}
