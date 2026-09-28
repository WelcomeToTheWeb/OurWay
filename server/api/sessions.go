package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/events"
	"ourway/server/models"
	"ourway/server/sessions"
	"ourway/server/store"
	"ourway/server/ws"
)

// SessionHandler handles session-related API endpoints.
type SessionHandler struct {
	store   *store.Store
	gateway *sessions.Gateway
	hub     *ws.Hub
}

// NewSessionHandler creates a new session handler.
func NewSessionHandler(store *store.Store, gateway *sessions.Gateway, hub *ws.Hub) *SessionHandler {
	return &SessionHandler{
		store:   store,
		gateway: gateway,
		hub:     hub,
	}
}

// StartSession starts a new remote session with a device.
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

	// Create session
	session := &models.Session{
		ID:       uuid.NewString(),
		DeviceID: deviceID,
		UserID:   userID,
		Status:   "pending",
	}
	if err := h.store.Sessions.Create(session); err != nil {
		c.JSON(500, gin.H{"error": "failed to create session"})
		return
	}

	// Create WebRTC peer connection
	if _, err := h.gateway.CreatePeerConnection(session.ID, deviceID, userID); err != nil {
		c.JSON(500, gin.H{"error": "failed to create peer connection"})
		return
	}

	// Create offer
	offerJSON, err := h.gateway.CreateOffer(session.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to create offer"})
		return
	}

	// Update session with offer
	if err := json.Unmarshal([]byte(offerJSON), &struct{}{}); err == nil {
		session.OfferSDP = offerJSON
		if err := h.store.Sessions.Update(session); err != nil {
			log.Printf("failed to update session with offer: %v", err)
		}
	}

	// Tell the device to start capturing: the agent's capture loop only
	// runs after it receives "session_start" on its WS connection. The
	// server URL is included so the agent knows where to upload frames.
	serverURL := "http://" + c.Request.Host
	if c.Request.TLS != nil {
		serverURL = "https://" + c.Request.Host
	}
	if err := h.hub.SendToDevice(device.DeviceKey, "session_start", gin.H{
		"session_id": session.ID,
		"server_url": serverURL,
	}); err != nil {
		log.Printf("sessions: failed to notify device of session start: %v", err)
	}

	events.Publish("session_started", map[string]interface{}{
		"session_id": session.ID,
		"device_id":  deviceID,
		"device":     device.Name,
		"user_id":    userID,
	})

	c.JSON(200, gin.H{
		"session": session,
		"offer":   offerJSON,
	})
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

// SubmitAnswer handles the browser's answer to the session offer.
// POST /api/sessions/:id/answer
func (h *SessionHandler) SubmitAnswer(c *gin.Context) {
	sessionID := c.Param("id")

	if _, ok := h.authorizeSessionAccess(c, sessionID); !ok {
		return
	}

	var req struct {
		Answer string `json:"answer" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	if err := h.gateway.SetRemoteAnswer(sessionID, req.Answer); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Update session status
	session, err := h.store.Sessions.GetByID(sessionID)
	if err != nil {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}

	session.Status = "active"
	if err := h.store.Sessions.Update(session); err != nil {
		log.Printf("failed to update session status: %v", err)
	}

	c.JSON(200, gin.H{"status": "ok"})
}

// AddICECandidate handles ICE candidate exchange.
// POST /api/sessions/:id/ice
func (h *SessionHandler) AddICECandidate(c *gin.Context) {
	sessionID := c.Param("id")

	if _, ok := h.authorizeSessionAccess(c, sessionID); !ok {
		return
	}

	var req struct {
		Candidate string `json:"candidate" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	if err := h.gateway.AddICECandidate(sessionID, req.Candidate); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

// EndSession ends an active session.
// DELETE /api/sessions/:id
func (h *SessionHandler) EndSession(c *gin.Context) {
	sessionID := c.Param("id")

	session, ok := h.authorizeSessionAccess(c, sessionID)
	if !ok {
		return
	}

	// Close the WebRTC peer connection if one is live on this instance.
	// After a server restart the in-memory gateway is empty — in that case
	// ending the session must still succeed (it is already effectively
	// over) so clients can reliably clean up.
	if _, live := h.gateway.GetSession(sessionID); live {
		if err := h.gateway.CloseSession(sessionID); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
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
// relays it to the browsers connected to the session. This is the reliable
// frame path while the WebRTC data channel is not complete.
// POST /api/sessions/:id/frame (device key auth via X-Device-Key, like /api/agent/*)
func (h *SessionHandler) ReportFrame(c *gin.Context) {
	deviceKey := c.GetHeader("X-Device-Key")
	if deviceKey == "" {
		c.JSON(401, gin.H{"error": "missing X-Device-Key header"})
		return
	}

	device, err := h.store.Devices.GetByKey(deviceKey)
	if err != nil {
		c.JSON(401, gin.H{"error": "unknown device key"})
		return
	}

	sessionID := c.Param("id")
	session, err := h.store.Sessions.GetByID(sessionID)
	if err != nil {
		c.JSON(404, gin.H{"error": "session not found"})
		return
	}
	if session.DeviceID != device.ID {
		c.JSON(403, gin.H{"error": "session does not belong to this device"})
		return
	}

	// Raw JPEG body, bounded to 10 MB.
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 10<<20))
	if err != nil || len(data) == 0 {
		c.JSON(400, gin.H{"error": "empty frame body"})
		return
	}

	// Relay only to the session owner's browser connection. Previously this
	// broadcast to every connected user, letting any authenticated client
	// watch another user's live session.
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
