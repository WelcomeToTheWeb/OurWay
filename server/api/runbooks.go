package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/automation"
	"ourway/server/events"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// RunbookHandler handles automation runbook endpoints.
type RunbookHandler struct {
	store  *store.Store
	hub    *ws.Hub
	engine *automation.Engine
}

// NewRunbookHandler creates a runbook handler.
func NewRunbookHandler(store *store.Store, hub *ws.Hub, engine *automation.Engine) *RunbookHandler {
	return &RunbookHandler{store: store, hub: hub, engine: engine}
}

type runbookRequest struct {
	Name        string `json:"name" binding:"required"`
	Scope       string `json:"scope"`
	ScopeValue  string `json:"scope_value"`
	Schedule    string `json:"schedule"`
	Command     string `json:"command" binding:"required"`
	Enabled     *bool  `json:"enabled"`
	WindowStart string `json:"window_start"`
	WindowHours int    `json:"window_hours"`
	Timezone    string `json:"timezone"`
}

func (h *RunbookHandler) applyDefaults(req *runbookRequest) {
	req.Scope = strings.ToLower(strings.TrimSpace(req.Scope))
	if req.Scope == "" {
		req.Scope = "all"
	}
	req.Schedule = strings.ToLower(strings.TrimSpace(req.Schedule))
	if req.Schedule == "" {
		req.Schedule = "weekly"
	}
	if req.Enabled == nil {
		t := true
		req.Enabled = &t
	}
}

// validateScope rejects unknown scopes so a typo does not silently match
// zero devices forever (same lesson as the patch-policy scope).
func validateScope(scope string) bool {
	switch scope {
	case "all", "tags", "devices":
		return true
	}
	return false
}

func validateSchedule(schedule string) bool {
	switch schedule {
	case "daily", "weekly", "monthly":
		return true
	}
	return false
}

// ListRunbooks returns all runbooks.
// GET /api/automation/runbooks
func (h *RunbookHandler) ListRunbooks(c *gin.Context) {
	runbooks, err := h.store.Runbooks.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runbooks": runbooks})
}

// CreateRunbook stores a new runbook.
// POST /api/automation/runbooks
func (h *RunbookHandler) CreateRunbook(c *gin.Context) {
	var req runbookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.applyDefaults(&req)
	if !validateScope(req.Scope) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope", "supported": []string{"all", "tags", "devices"}})
		return
	}
	if !validateSchedule(req.Schedule) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule", "supported": []string{"daily", "weekly", "monthly"}})
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "command must not be empty"})
		return
	}
	runbook := &models.Runbook{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Scope:       req.Scope,
		ScopeValue:  req.ScopeValue,
		Schedule:    req.Schedule,
		Command:     req.Command,
		Enabled:     *req.Enabled,
		WindowStart: req.WindowStart,
		WindowHours: req.WindowHours,
		Timezone:    req.Timezone,
	}
	if err := h.store.Runbooks.Create(runbook); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"runbook": runbook})
}

// UpdateRunbook saves changes to a runbook.
// PUT /api/automation/runbooks/:id
func (h *RunbookHandler) UpdateRunbook(c *gin.Context) {
	runbook, err := h.store.Runbooks.GetByID(c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "runbook not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	var req runbookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.applyDefaults(&req)
	if !validateScope(req.Scope) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scope", "supported": []string{"all", "tags", "devices"}})
		return
	}
	if !validateSchedule(req.Schedule) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule", "supported": []string{"daily", "weekly", "monthly"}})
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "command must not be empty"})
		return
	}
	runbook.Name = req.Name
	runbook.Scope = req.Scope
	runbook.ScopeValue = req.ScopeValue
	runbook.Schedule = req.Schedule
	runbook.Command = req.Command
	runbook.Enabled = *req.Enabled
	runbook.WindowStart = req.WindowStart
	runbook.WindowHours = req.WindowHours
	runbook.Timezone = req.Timezone
	if err := h.store.Runbooks.Update(runbook); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runbook": runbook})
}

// DeleteRunbook removes a runbook.
// DELETE /api/automation/runbooks/:id
func (h *RunbookHandler) DeleteRunbook(c *gin.Context) {
	err := h.store.Runbooks.Delete(c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "runbook not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// RunNow dispatches the runbook immediately, bypassing schedule/window
// and Enabled (a disabled runbook may still be run manually).
// POST /api/automation/runbooks/:id/run
func (h *RunbookHandler) RunNow(c *gin.Context) {
	runbook, err := h.store.Runbooks.GetByID(c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "runbook not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	if h.engine == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "automation engine unavailable"})
		return
	}
	h.engine.DispatchNow(runbook)
	c.JSON(http.StatusOK, gin.H{"status": "dispatched"})
}

// ListRuns returns a runbook's recent runs.
// GET /api/automation/runbooks/:id/runs
func (h *RunbookHandler) ListRuns(c *gin.Context) {
	runs, err := h.store.Runbooks.ListRunsByRunbook(c.Param("id"), 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

// ReportRunResult receives a runbook run result from an agent
// (X-Device-Key header auth, like /api/agent/deployments/result).
// POST /api/agent/runbooks/result
func (h *RunbookHandler) ReportRunResult(c *gin.Context) {
	var req struct {
		RunID    string `json:"run_id" binding:"required"`
		DeviceID string `json:"device_id" binding:"required"`
		Success  bool   `json:"success"`
		ExitCode int    `json:"exit_code"`
		Output   string `json:"output"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	deviceKey := c.GetHeader("X-Device-Key")
	if deviceKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing X-Device-Key header"})
		return
	}
	device, err := h.store.Devices.GetByKey(deviceKey)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unknown device key"})
		return
	}
	if req.DeviceID != device.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "device_id does not match device key"})
		return
	}
	run, err := h.store.Runbooks.GetRun(req.RunID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	if run.DeviceID != req.DeviceID {
		c.JSON(http.StatusForbidden, gin.H{"error": "run does not belong to this device"})
		return
	}
	status := "failed"
	if req.Success {
		status = "success"
	}
	exitCode := req.ExitCode
	run.Status = status
	run.ExitCode = &exitCode
	run.Output = req.Output
	if len(run.Output) > 65536 {
		run.Output = run.Output[:65536]
	}
	now := time.Now()
	run.FinishedAt = &now
	if err := h.store.Runbooks.UpdateRun(run); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	events.Publish("runbook_run_finished", map[string]interface{}{
		"runbook_id": run.RunbookID,
		"run_id":     run.ID,
		"device_id":  run.DeviceID,
		"status":     status,
		"exit_code":  req.ExitCode,
	})
	c.JSON(http.StatusOK, gin.H{"status": "recorded"})
}
