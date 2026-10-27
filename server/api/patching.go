package api

import (
	"errors"
	"regexp"
	"strings"
	"time"

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
	// A unit's outstanding updates name its software and its version gaps, so
	// they are scoped with the unit.
	device, ok := ScopedDevice(c, h.store)
	if !ok {
		return
	}
	deviceID := device.ID

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
	device, ok := ScopedDevice(c, h.store)
	if !ok {
		return
	}
	defer auditForAnswer(c, h.store, models.AuditPatchScan, DeviceTarget(device), nil)
	deviceID := device.ID

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

	// Approval is what lets an install reach a machine, so it is scoped through
	// the unit the update belongs to — and the out-of-scope answer is the
	// update-not-found answer, not "not your client".
	if code, body := h.checkUpdateScope(c, update); code != 0 {
		// An approval attempt at an update on another tenant's unit. The
		// answer is the record, and the record is the row.
		auditForAnswer(c, h.store, models.AuditPatchApprove, AuditTarget{Type: "update", ID: updateID}, nil)
		c.JSON(code, body)
		return
	}
	defer auditForAnswer(c, h.store, models.AuditPatchApprove, h.updateTarget(update), map[string]any{
		"title": update.Title,
	})

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
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)

	policies, err := h.store.PatchPolicies.ListAll()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list policies"})
		return
	}

	// A policy is a standing instruction to install software on units, so a
	// scoped caller sees the policies that can reach their units: a policy over
	// the whole installation reaches them, and a tag- or device-scoped policy
	// reaches them when one of their units is in its targets. A policy aimed at
	// another client's units says nothing about the fleet they manage, and
	// listing it would disclose the other client's naming and layout.
	if !scope.all {
		devices, err := h.store.Devices.ListScoped(scope.ClientIDs(), scope.IncludeUnassigned())
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to resolve client scope"})
			return
		}
		policies = policiesReachingDevices(policies, devices)
	}

	c.JSON(200, gin.H{"policies": policies})
}

// policiesReachingDevices keeps the policies that can act on `devices`.
func policiesReachingDevices(policies []models.PatchPolicy, devices []models.Device) []models.PatchPolicy {
	tags := map[string]bool{}
	ids := map[string]bool{}
	for _, d := range devices {
		ids[d.ID] = true
		for _, tag := range d.Tags {
			tags[tag] = true
		}
	}
	kept := make([]models.PatchPolicy, 0, len(policies))
	for _, p := range policies {
		switch p.Scope {
		case "all", "":
			kept = append(kept, p)
		case "tags":
			for _, tag := range strings.Split(p.ScopeValue, ",") {
				if tags[strings.TrimSpace(tag)] {
					kept = append(kept, p)
					break
				}
			}
		case "devices":
			for _, id := range strings.Split(p.ScopeValue, ",") {
				if ids[strings.TrimSpace(id)] {
					kept = append(kept, p)
					break
				}
			}
		default:
			// An unrecognised scope reaches nothing we can prove, so it is not
			// offered: the safe answer for a scoped caller is the smaller list.
			continue
		}
	}
	return kept
}

// policyRequest is the writable shape of a patch policy.
type policyRequest struct {
	Name               string `json:"name" binding:"required"`
	Scope              string `json:"scope"`
	ScopeValue         string `json:"scope_value"`
	Schedule           string `json:"schedule"`
	AutoReboot         bool   `json:"auto_reboot"`
	ApprovalRequired   *bool  `json:"approval_required"`
	MaxDevicesPerBatch int    `json:"max_devices_per_batch"`
	WindowStart        string `json:"window_start"`
	WindowHours        int    `json:"window_hours"`
	Timezone           string `json:"timezone"`
}

var windowStartRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// apply validates req and copies it onto policy, applying defaults for
// anything left blank. It returns a client-facing error message, or "".
func (req *policyRequest) apply(policy *models.PatchPolicy) string {
	scope := req.Scope
	if scope == "" {
		scope = "all"
	}
	if scope != "all" && scope != "tags" && scope != "devices" {
		return "scope must be all, tags or devices"
	}
	schedule := req.Schedule
	if schedule == "" {
		schedule = "weekly"
	}
	if schedule != "daily" && schedule != "weekly" && schedule != "monthly" {
		return "schedule must be daily, weekly or monthly"
	}
	maxBatch := req.MaxDevicesPerBatch
	if maxBatch == 0 {
		maxBatch = 10
	}
	if maxBatch < 0 {
		return "max_devices_per_batch must be positive"
	}
	windowStart := req.WindowStart
	if windowStart == "" {
		windowStart = "02:00"
	}
	if !windowStartRe.MatchString(windowStart) {
		return "window_start must be HH:MM (24-hour)"
	}
	windowHours := req.WindowHours
	if windowHours == 0 {
		windowHours = 4
	}
	if windowHours < 1 || windowHours > 24 {
		return "window_hours must be between 1 and 24"
	}
	tz := req.Timezone
	if tz == "" {
		tz = "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "unknown timezone"
	}
	// The model carries no gorm default for this bool (a default tag
	// silently overrode an explicit false on create), so apply the safe
	// default here: approval is required unless the request opts out.
	approvalRequired := true
	if req.ApprovalRequired != nil {
		approvalRequired = *req.ApprovalRequired
	}
	policy.Name = req.Name
	policy.Scope = scope
	policy.ScopeValue = req.ScopeValue
	policy.Schedule = schedule
	policy.AutoReboot = req.AutoReboot
	policy.ApprovalRequired = approvalRequired
	policy.MaxDevicesPerBatch = maxBatch
	policy.WindowStart = windowStart
	policy.WindowHours = windowHours
	policy.Timezone = tz
	return ""
}

// CreatePolicy creates a new patch policy.
// POST /api/patch/policies
func (h *PatchHandler) CreatePolicy(c *gin.Context) {
	var req policyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}
	policy := &models.PatchPolicy{ID: uuid.New().String()}
	if msg := req.apply(policy); msg != "" {
		c.JSON(400, gin.H{"error": msg})
		return
	}
	defer auditForAnswer(c, h.store, models.AuditPolicyCreate, AuditTarget{Type: "patch-policy", ID: policy.ID}, map[string]any{
		"name":  policy.Name,
		"scope": policy.Scope,
	})
	if err := h.store.PatchPolicies.Create(policy); err != nil {
		c.JSON(500, gin.H{"error": "failed to create policy"})
		return
	}
	c.JSON(201, gin.H{"policy": policy})
}

// UpdatePolicy replaces a policy's settings. Changing the schedule or
// window does not reset LastRunAt, so a policy that already ran today will
// not run again until its next window.
// PUT /api/patch/policies/:id
func (h *PatchHandler) UpdatePolicy(c *gin.Context) {
	var req policyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}
	policy, err := h.store.PatchPolicies.GetByID(c.Param("id"))
	if err != nil {
		c.JSON(404, gin.H{"error": "policy not found"})
		return
	}
	if msg := req.apply(policy); msg != "" {
		c.JSON(400, gin.H{"error": msg})
		return
	}
	defer auditForAnswer(c, h.store, models.AuditPolicyUpdate, AuditTarget{Type: "patch-policy", ID: policy.ID}, map[string]any{
		"name":  policy.Name,
		"scope": policy.Scope,
	})
	if err := h.store.PatchPolicies.Update(policy); err != nil {
		c.JSON(500, gin.H{"error": "failed to update policy"})
		return
	}
	c.JSON(200, gin.H{"policy": policy})
}

// DeletePolicy removes a policy (queued deployments for it are dropped the
// next time the queue is processed).
// DELETE /api/patch/policies/:id
func (h *PatchHandler) DeletePolicy(c *gin.Context) {
	if _, err := h.store.PatchPolicies.GetByID(c.Param("id")); err != nil {
		c.JSON(404, gin.H{"error": "policy not found"})
		return
	}
	defer auditForAnswer(c, h.store, models.AuditPolicyDelete, AuditTarget{Type: "patch-policy", ID: c.Param("id")}, nil)
	if err := h.store.PatchPolicies.Delete(c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": "failed to delete policy"})
		return
	}
	c.JSON(200, gin.H{"status": "deleted"})
}

// ListDeployments returns all patch deployments.
// GET /api/patch/deployments
func (h *PatchHandler) ListDeployments(c *gin.Context) {
	// Finalize any deployments that timed out (so the UI never shows a
	// deployment stuck in "running" past its deadline).
	h.deployer.FinalizeTimedOut()

	_, visible, restricted, err := h.fleetScope(c)
	if err != nil {
		ScopeError(c)
		return
	}

	deployments, err := h.store.PatchDeployments.ListAll()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list deployments"})
		return
	}
	if restricted {
		deployments = keepDeploymentsOnVisibleDevices(deployments, visible)
	}

	c.JSON(200, gin.H{"deployments": deployments})
}

// ListDeploymentResults returns the recorded per-device outcomes (deploy
// and rollback phases) for one deployment.
// GET /api/patch/deployments/:id/results
func (h *PatchHandler) ListDeploymentResults(c *gin.Context) {
	// Per-device outcomes name the units and what was installed on them, so the
	// deployment's unit set is checked before any of it is read.
	if _, ok := h.scopedDeployment(c); !ok {
		return
	}
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

	// A deployment installs onto machines, so every target is validated before
	// anything is created.
	// The deploy is recorded with the targets it was given, before any of them
	// is touched. A refused bulk command is a row naming the ids it refused:
	// that is the event an MSP reads the ledger for, and the 400 naming them is
	// visible only to the caller who made the request.
	defer auditForAnswer(c, h.store, models.AuditPatchDeploy, AuditTarget{Type: "device-batch"}, map[string]any{
		"device_ids": req.DeviceIDs,
	})
	if code, body := h.validateDeviceTargets(c, req.DeviceIDs); code != 0 {
		auditNameRefusedTargets(c, refusedTargetsOf(body))
		c.JSON(code, body)
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

	// A rollback runs commands on the units in the deployment, so the
	// deployment is scoped through all of them: a deployment spanning two
	// clients is not a deployment the manager of one of them can roll back.
	deployment, ok := h.scopedDeployment(c)
	if !ok {
		return
	}

	// An explicit device list is validated on top of that — a caller who names
	// units outside this deployment's reach must fail naming them, not quietly
	// roll back the subset they were allowed to name.
	defer auditForAnswer(c, h.store, models.AuditPatchRollback, AuditTarget{Type: "deployment", ID: deploymentID}, map[string]any{
		"device_ids": req.DeviceIDs,
	})
	if code, body := h.validateDeviceTargets(c, req.DeviceIDs); code != 0 {
		auditNameRefusedTargets(c, refusedTargetsOf(body))
		c.JSON(code, body)
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
