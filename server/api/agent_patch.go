package api

import (
	"log"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ourway/server/events"
	"ourway/server/models"
	"ourway/server/patching"
	"ourway/server/store"
)

// AgentPatchHandler handles agent patch report endpoints.
type AgentPatchHandler struct {
	store    *store.Store
	deployer *patching.Deployer
}

// NewAgentPatchHandler creates a new agent patch handler.
func NewAgentPatchHandler(store *store.Store, deployer *patching.Deployer) *AgentPatchHandler {
	return &AgentPatchHandler{
		store:    store,
		deployer: deployer,
	}
}

// verifyDeviceKey authenticates the calling agent via its X-Device-Key
// header and verifies the claimed device_id matches the key's owner.
// Writes the 401/403 response on failure.
func (h *AgentPatchHandler) verifyDeviceKey(c *gin.Context, deviceID string) bool {
	deviceKey := c.GetHeader("X-Device-Key")
	if deviceKey == "" {
		c.JSON(401, gin.H{"error": "missing X-Device-Key header"})
		return false
	}

	device, err := h.store.Devices.GetByKey(deviceKey)
	if err != nil {
		c.JSON(401, gin.H{"error": "unknown device key"})
		return false
	}

	if deviceID != device.ID {
		c.JSON(403, gin.H{"error": "device_id does not match device key"})
		return false
	}

	return true
}

// ReportUpdate receives an update report from an agent.
// POST /api/agent/updates
func (h *AgentPatchHandler) ReportUpdate(c *gin.Context) {
	var req struct {
		DeviceID   string `json:"device_id" binding:"required"`
		Source     string `json:"source" binding:"required"`
		Title      string `json:"title" binding:"required"`
		Version    string `json:"version"`
		SizeBytes  int64  `json:"size_bytes"`
		ExternalID string `json:"external_id"`
		KB         string `json:"kb"`
		Severity   string `json:"severity"`
		Category   string `json:"category"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	if !h.verifyDeviceKey(c, req.DeviceID) {
		return
	}

	update := &models.SoftwareUpdate{
		ID:         uuid.NewString(),
		ExternalID: req.ExternalID,
		KB:         req.KB,
		Severity:   normalizeSeverity(req.Severity),
		Category:   req.Category,
		DeviceID:   req.DeviceID,
		Source:     req.Source,
		Title:      req.Title,
		Version:    req.Version,
		SizeBytes:  req.SizeBytes,
		Status:     "detected",
	}

	if err := h.store.SoftwareUpdates.UpsertDetected(update); err != nil {
		c.JSON(500, gin.H{"error": "failed to create update record"})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

// ReportDeploymentResult receives a deployment result from an agent.
// POST /api/agent/deployments/result
func (h *AgentPatchHandler) ReportDeploymentResult(c *gin.Context) {
	var req struct {
		DeviceID     string `json:"device_id" binding:"required"`
		DeploymentID string `json:"deployment_id" binding:"required"`
		Result       string `json:"result" binding:"required"`
		Kind         string `json:"kind"`
		Message      string `json:"message"`
		// RebootRequired is set by the agent when the install needs a
		// reboot to complete.
		RebootRequired bool `json:"reboot_required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	if !h.verifyDeviceKey(c, req.DeviceID) {
		return
	}

	if err := h.deployer.ReportResult(req.DeploymentID, req.DeviceID, req.Kind, req.Result, req.Message); err != nil {
		c.JSON(500, gin.H{"error": "failed to record deployment result"})
		return
	}

	if req.RebootRequired && kindOrDefault(req.Kind) == "deploy" && req.Result == "success" {
		if dev, err := h.store.Devices.GetByID(req.DeviceID); err == nil && !dev.RebootPending {
			dev.RebootPending = true
			if err := h.store.Devices.Update(dev); err != nil {
				log.Printf("patching: failed to flag reboot pending for %s: %v", req.DeviceID, err)
			}
		}
	}

	events.Publish("patch_deployed", map[string]interface{}{
		"deployment_id": req.DeploymentID,
		"device_id":     req.DeviceID,
		"result":        req.Result,
		"kind":          kindOrDefault(req.Kind),
		"message":       req.Message,
	})

	c.JSON(200, gin.H{"status": "ok"})
}

// kindOrDefault normalizes an empty kind to "deploy".
func kindOrDefault(kind string) string {
	if kind == "" {
		return "deploy"
	}
	return kind
}

// normalizeSeverity maps an agent-supplied severity onto the known set.
func normalizeSeverity(s string) string {
	switch v := strings.ToLower(strings.TrimSpace(s)); v {
	case "critical", "important", "moderate", "low":
		return v
	default:
		return "unspecified"
	}
}
