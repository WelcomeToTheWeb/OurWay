package api

import (
	"github.com/gin-gonic/gin"

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
		DeviceID  string `json:"device_id" binding:"required"`
		Source    string `json:"source" binding:"required"`
		Title     string `json:"title" binding:"required"`
		Version   string `json:"version"`
		SizeBytes int64  `json:"size_bytes"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	if !h.verifyDeviceKey(c, req.DeviceID) {
		return
	}

	update := &models.SoftwareUpdate{
		DeviceID:  req.DeviceID,
		Source:    req.Source,
		Title:     req.Title,
		Version:   req.Version,
		SizeBytes: req.SizeBytes,
		Status:    "detected",
	}

	if err := h.store.SoftwareUpdates.Create(update); err != nil {
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
