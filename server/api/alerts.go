package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ourway/server/events"
	"ourway/server/store"
)

// AlertHandler handles alert-related endpoints.
type AlertHandler struct {
	store *store.Store
}

// NewAlertHandler creates an alert handler.
func NewAlertHandler(store *store.Store) *AlertHandler {
	return &AlertHandler{store: store}
}

// ListAlerts returns all alerts, optionally filtered by severity or resolved status.
func (h *AlertHandler) ListAlerts(c *gin.Context) {
	severity := c.Query("severity")
	resolved := c.Query("resolved")

	var deviceID *string
	if dev := c.Query("device_id"); dev != "" {
		deviceID = &dev
	}

	unresolvedOnly := resolved == "false"

	alerts, err := h.store.Alerts.List(deviceID, unresolvedOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list alerts"})
		return
	}

	// Filter by severity if specified
	if severity != "" {
		filtered := make([]interface{}, 0)
		for _, a := range alerts {
			if a.Severity == severity {
				filtered = append(filtered, a)
			}
		}
		c.JSON(http.StatusOK, gin.H{"alerts": filtered})
		return
	}

	c.JSON(http.StatusOK, gin.H{"alerts": alerts})
}

// ResolveAlert marks an alert as resolved.
func (h *AlertHandler) ResolveAlert(c *gin.Context) {
	id := c.Param("id")
	alert, err := h.store.Alerts.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "alert not found"})
		return
	}
	if err := h.store.Alerts.MarkResolved(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve alert"})
		return
	}
	events.Publish("alert_resolved", map[string]interface{}{
		"alert_id":  id,
		"device_id": alert.DeviceID,
		"device":    alert.DeviceName,
		"severity":  alert.Severity,
		"message":   alert.Message,
	})
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// AcknowledgeAlert marks an alert as acknowledged.
func (h *AlertHandler) AcknowledgeAlert(c *gin.Context) {
	id := c.Param("id")
	userID, _ := c.Get("user_id")
	if err := h.store.Alerts.Acknowledge(id, userID.(string)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to acknowledge alert"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// AssignAlert assigns an alert to a user.
func (h *AlertHandler) AssignAlert(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		UserID string `json:"user_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.store.Alerts.Assign(id, req.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign alert"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
