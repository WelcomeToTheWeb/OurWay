package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"ourway/server/patching"
	"ourway/server/store"
)

// RebootHandler handles reboot API endpoints.
type RebootHandler struct {
	store    *store.Store
	rebooter *patching.Rebooter
}

// NewRebootHandler creates a new reboot handler.
func NewRebootHandler(store *store.Store, rebooter *patching.Rebooter) *RebootHandler {
	return &RebootHandler{
		store:    store,
		rebooter: rebooter,
	}
}

// RebootDevice triggers a reboot of a device.
// POST /api/devices/:id/reboot
func (h *RebootHandler) RebootDevice(c *gin.Context) {
	deviceID := c.Param("id")

	var req struct {
		DelaySeconds int `json:"delay_seconds"`
	}
	// Body is optional; a present but malformed body must not silently
	// trigger an immediate reboot (M3).
	if body, err := io.ReadAll(c.Request.Body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	} else if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
	}

	device, err := h.store.Devices.GetByID(deviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	if device.Status != "online" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}

	var sendErr error
	if req.DelaySeconds > 0 {
		sendErr = h.rebooter.RebootWithDelay(device.DeviceKey, req.DelaySeconds)
	} else {
		sendErr = h.rebooter.RebootDevice(device.DeviceKey)
	}
	if sendErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to reach device"})
		return
	}

	c.JSON(200, gin.H{"status": "reboot_initiated"})
}
