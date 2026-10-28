package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ourway/server/events"
	"ourway/server/models"
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

	// An alert is a statement about a unit, so it is scoped with the unit: the
	// disk-full page on a client you were removed from must disappear from this
	// list. The client filter is in the SQL (`AlertStore.ListScoped`), not a
	// trim of the full table in the handler — 2.3-2's criterion, and the
	// difference between a scoped request and an unscoped one at fleet size.
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return
	}
	scope.ApplyScopeHeader(c)

	var alerts []models.Alert
	if scope.all {
		alerts, err = h.store.Alerts.List(deviceID, unresolvedOnly)
	} else {
		// The client set goes into the query; the units it names are the
		// planner's problem (2.3-8).
		alerts, err = h.store.Alerts.ListScoped(scope.ClientIDs(), scope.IncludeUnassigned(), deviceID, unresolvedOnly)
	}
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

// AlertNotFound is the single 404 for an alert route, for an alert that does
// not exist and for one whose unit is out of the caller's scope. Same rule as
// devices (see scope.go): a body that distinguishes them is a directory of
// another client's incidents.
func AlertNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "alert not found"})
}

// scopedAlert loads an alert and answers for it when the caller may not see the
// unit it belongs to.
func (h *AlertHandler) scopedAlert(c *gin.Context) (*models.Alert, bool) {
	alert, err := h.store.Alerts.GetByID(c.Param("id"))
	if err != nil {
		AlertNotFound(c)
		return nil, false
	}
	scope, err := ClientScope(c, h.store)
	if err != nil {
		ScopeError(c)
		return nil, false
	}
	scope.ApplyScopeHeader(c)
	ok, err := scope.AllowsDeviceID(c, alert.DeviceID, h.store)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve client scope"})
		return nil, false
	}
	if !ok {
		AlertNotFound(c)
		return nil, false
	}
	return alert, true
}

// alertTarget names an alert's unit, and the unit's client, so the record
// carries the tenant at the time of the act. An alert has no client of its own
// — it points at a unit — so this is the same derivation the fleet reads use.
func (h *AlertHandler) alertTarget(alert *models.Alert) AuditTarget {
	if device, err := h.store.Devices.GetByID(alert.DeviceID); err == nil && device != nil {
		return DeviceTarget(device)
	}
	return AuditTarget{Type: "alert", ID: alert.ID}
}

// ResolveAlert marks an alert as resolved.
func (h *AlertHandler) ResolveAlert(c *gin.Context) {
	id := c.Param("id")
	alert, ok := h.scopedAlert(c)
	if !ok {
		return
	}
	defer auditForAnswer(c, h.store, models.AuditAlertResolve, h.alertTarget(alert), nil)
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
	alert, ok := h.scopedAlert(c)
	if !ok {
		return
	}
	defer auditForAnswer(c, h.store, models.AuditAlertAcknowledge, h.alertTarget(alert), nil)
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
	alert, ok := h.scopedAlert(c)
	if !ok {
		return
	}
	defer auditForAnswer(c, h.store, models.AuditAlertAssign, h.alertTarget(alert), nil)

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
