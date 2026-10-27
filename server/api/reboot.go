package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
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
	// A reboot is the most destructive act the console can send to a unit, so
	// it walks the same door as reading that unit: out of scope answers 404, the
	// answer for a unit that does not exist. Scope is checked *before* the body
	// is read, so an attempt at a foreign unit is refused on identity, not on
	// payload validation — the order matters for 2.3-6, which logs the actions
	// this handler goes on to perform.
	device, ok := ScopedDevice(c, h.store)
	if !ok {
		// A reboot attempt at a unit outside the caller's scope. `ScopedDevice`
		// has already answered, so the status the caller got is on the writer:
		// 2.3-6's rule is that refusals are rows, and a reboot is the most
		// destructive thing the console can send.
		auditForAnswer(c, h.store, models.AuditDeviceReboot, AuditTarget{Type: "device", ID: c.Param("id")}, nil)
		return
	}
	// Deferred so the outcome is the answer: a reboot the agent never received
	// is not a reboot, and a ledger that says one happened is a ledger that is
	// trusted for exactly one incident and then ignored.
	defer auditForAnswer(c, h.store, models.AuditDeviceReboot, DeviceTarget(device), nil)
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
