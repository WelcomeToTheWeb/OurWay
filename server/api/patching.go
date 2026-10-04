package api

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/models"
	"ourway/server/patching"
	"ourway/server/store"
)

// PatchHandler handles patch management API endpoints.
type PatchHandler struct {
	store      *store.Store
	scanner    *patching.Scanner
	deployer   *patching.Deployer
	rollbacker *patching.RollbackManager
}

// NewPatchHandler creates a new patch handler.
func NewPatchHandler(store *store.Store, scanner *patching.Scanner, deployer *patching.Deployer, rollbacker *patching.RollbackManager) *PatchHandler {
	return &PatchHandler{
		store:      store,
		scanner:    scanner,
		deployer:   deployer,
		rollbacker: rollbacker,
	}
}

// ListUpdates returns software updates for a device.
// GET /api/devices/:id/updates
func (h *PatchHandler) ListUpdates(c *gin.Context) {
	deviceID := c.Param("id")

	updates, err := h.store.SoftwareUpdates.ListByDevice(deviceID)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list updates"})
		return
	}

	c.JSON(200, gin.H{"updates": updates})
}

// ScanDevice triggers an update scan on a device.
// POST /api/devices/:id/updates/scan
func (h *PatchHandler) ScanDevice(c *gin.Context) {
	deviceID := c.Param("id")

	// Verify device exists
	if _, err := h.store.Devices.GetByID(deviceID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"error": "device not found"})
			return
		}
		c.JSON(500, gin.H{"error": "failed to get device"})
		return
	}

	// Scan just this device.
	h.scanner.ScanDeviceIDs([]string{deviceID})

	c.JSON(200, gin.H{"status": "scan_started"})
}

// ApproveUpdate transitions a detected update to approved.
// POST /api/updates/:id/approve
func (h *PatchHandler) ApproveUpdate(c *gin.Context) {
	updateID := c.Param("id")

	update, err := h.store.SoftwareUpdates.GetByID(updateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"error": "update not found"})
			return
		}
		c.JSON(500, gin.H{"error": "failed to get update"})
		return
	}

	if update.Status != "detected" {
		c.JSON(409, gin.H{"error": "only detected updates can be approved", "status": update.Status})
		return
	}

	if _, err := h.store.SoftwareUpdates.MarkApproved(updateID); err != nil {
		c.JSON(500, gin.H{"error": "failed to approve update"})
		return
	}

	updated, err := h.store.SoftwareUpdates.GetByID(updateID)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get update"})
		return
	}

	c.JSON(200, gin.H{"update": updated})
}

// ListPolicies returns all patch policies.
// GET /api/patch/policies
func (h *PatchHandler) ListPolicies(c *gin.Context) {
	policies, err := h.store.PatchPolicies.ListAll()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list policies"})
		return
	}

	c.JSON(200, gin.H{"policies": policies})
}

// CreatePolicy creates a new patch policy.
// POST /api/patch/policies
func (h *PatchHandler) CreatePolicy(c *gin.Context) {
	var req struct {
		Name               string `json:"name" binding:"required"`
		Scope              string `json:"scope"`
		ScopeValue         string `json:"scope_value"`
		Schedule           string `json:"schedule"`
		AutoReboot         bool   `json:"auto_reboot"`
		ApprovalRequired   *bool  `json:"approval_required"`
		MaxDevicesPerBatch int    `json:"max_devices_per_batch"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	scope := req.Scope
	if scope == "" {
		scope = "all"
	}
	schedule := req.Schedule
	if schedule == "" {
		schedule = "weekly"
	}
	maxBatch := req.MaxDevicesPerBatch
	if maxBatch == 0 {
		maxBatch = 10
	}
	// The model carries no gorm default for this bool (a default tag
	// silently overrode an explicit false on create), so apply the safe
	// default here: approval is required unless the request opts out.
	approvalRequired := true
	if req.ApprovalRequired != nil {
		approvalRequired = *req.ApprovalRequired
	}


	policy := &models.PatchPolicy{
		ID:                 uuid.New().String(),
		Name:               req.Name,
		Scope:              scope,
		ScopeValue:         req.ScopeValue,
		Schedule:           schedule,
		AutoReboot:         req.AutoReboot,
		ApprovalRequired:   approvalRequired,
		MaxDevicesPerBatch: maxBatch,
	}

	if err := h.store.PatchPolicies.Create(policy); err != nil {
		c.JSON(500, gin.H{"error": "failed to create policy"})
		return
	}

	c.JSON(201, gin.H{"policy": policy})
}

// ListDeployments returns all patch deployments.
// GET /api/patch/deployments
func (h *PatchHandler) ListDeployments(c *gin.Context) {
	// Finalize any deployments that timed out (so the UI never shows a
	// deployment stuck in "running" past its deadline).
	h.deployer.FinalizeTimedOut()

	deployments, err := h.store.PatchDeployments.ListAll()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list deployments"})
		return
	}

	c.JSON(200, gin.H{"deployments": deployments})
}

// ListDeploymentResults returns the recorded per-device outcomes (deploy
// and rollback phases) for one deployment.
// GET /api/patch/deployments/:id/results
func (h *PatchHandler) ListDeploymentResults(c *gin.Context) {
	results, err := h.store.DeploymentResults.ListByDeployment(c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list deployment results"})
		return
	}
	if results == nil {
		results = []models.DeploymentResult{}
	}
	c.JSON(200, gin.H{"results": results})
}

// DeployNow triggers an immediate deployment.
// POST /api/patch/deploy
func (h *PatchHandler) DeployNow(c *gin.Context) {
	var req struct {
		DeviceIDs []string `json:"device_ids"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	// If no device IDs, deploy to all devices with approved updates
	if len(req.DeviceIDs) == 0 {
		updates, err := h.store.SoftwareUpdates.ListByStatus("approved")
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to list updates"})
			return
		}

		// Get unique device IDs from updates
		deviceSet := make(map[string]bool)
		for _, update := range updates {
			deviceSet[update.DeviceID] = true
		}

		req.DeviceIDs = make([]string, 0, len(deviceSet))
		for deviceID := range deviceSet {
			req.DeviceIDs = append(req.DeviceIDs, deviceID)
		}
	}

	deploymentID, err := h.deployer.DeployToDevices(c.Request.Context(), req.DeviceIDs)
	if err != nil {
		// None of the requested devices have approved updates: this is a
		// client error, not a server failure.
		if errors.Is(err, patching.ErrNoApprovedUpdates) {
			c.JSON(400, gin.H{"error": "no approved updates for the requested devices"})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"deployment_id": deploymentID, "status": "deploying"})
}

// RollbackDeployment triggers a rollback for a completed/failed deployment.
// POST /api/patch/deployments/:id/rollback
func (h *PatchHandler) RollbackDeployment(c *gin.Context) {
	deploymentID := c.Param("id")

	var req struct {
		DeviceIDs []string `json:"device_ids"`
	}

	// Bind is optional - if no body or no device_ids, rollback to all devices
	c.ShouldBindJSON(&req)

	// Verify deployment exists
	deployment, err := h.store.PatchDeployments.GetByID(deploymentID)
	if err != nil || deployment == nil {
		c.JSON(404, gin.H{"error": "deployment not found"})
		return
	}

	// If no specific devices requested, target the devices in this deployment
	if len(req.DeviceIDs) == 0 {
		req.DeviceIDs = deployment.DeviceIDs
	}

	status, err := h.rollbacker.RollbackDeployment(deploymentID, req.DeviceIDs)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"status": status, "deployment_id": deploymentID, "devices": len(req.DeviceIDs)})
}
