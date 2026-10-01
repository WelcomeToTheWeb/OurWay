package api

import (
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
	if err := c.ShouldBindJSON(&req); err != nil {
		// Not required
	}

	device, err := h.store.Devices.GetByID(deviceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "device not found"})
		return
	}

	if req.DelaySeconds > 0 {
		h.rebooter.RebootWithDelay(device.DeviceKey, req.DelaySeconds)
	} else {
		h.rebooter.RebootDevice(device.DeviceKey)
	}

	c.JSON(200, gin.H{"status": "reboot_initiated"})
}
