package api

import (
	"encoding/json"
	"errors"
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

// SubmitAnswer handles the browser's answer to the session offer.
// POST /api/sessions/:id/answer
func (h *SessionHandler) SubmitAnswer(c *gin.Context) {
	sessionID := c.Param("id")

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

	if err := h.gateway.CloseSession(sessionID); err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}

	if err := h.store.Sessions.EndSession(sessionID); err != nil {
		c.JSON(500, gin.H{"error": "failed to end session"})
		return
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

	// Get session to find device
	session, err := h.store.Sessions.GetByID(sessionID)
	if err != nil {
		c.JSON(404, gin.H{"error": "session not found"})
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
