package api

import (
	"crypto/subtle"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ourway/server/alerts"
	"ourway/server/events"
	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// DeviceHandler handles device-related endpoints.
type DeviceHandler struct {
	store         *store.Store
	hub           *ws.Hub
	alerts        *alerts.Engine
	metricHistory *store.MetricHistoryStore
	// enrollSecret, when non-empty, gates new device registration and
	// keyless re-registration (C4).
	enrollSecret string
}

// NewDeviceHandler creates a device handler.
func NewDeviceHandler(store *store.Store, hub *ws.Hub, engine *alerts.Engine, metricHistory *store.MetricHistoryStore, enrollSecret string) *DeviceHandler {
	return &DeviceHandler{store: store, hub: hub, alerts: engine, metricHistory: metricHistory, enrollSecret: enrollSecret}
}

// validEnrollSecret reports whether the request carries the enrollment
// secret (header X-Enrollment-Secret) and it matches the configured
// value, compared in constant time.
func (h *DeviceHandler) validEnrollSecret(c *gin.Context) bool {
	if h.enrollSecret == "" {
		return false
	}
	got := c.GetHeader("X-Enrollment-Secret")
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.enrollSecret)) == 1
}

// RegisterDevice handles agent device registration.
// Idempotent: re-registering the same physical machine (same hostname +
// private IP, e.g. the installer run twice) returns the existing device and
// its key instead of creating a duplicate.
func (h *DeviceHandler) RegisterDevice(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		Hostname     string `json:"hostname" binding:"required"`
		OS           string `json:"os" binding:"required"`
		Arch         string `json:"arch" binding:"required"`
		AgentVersion string `json:"agent_version"`
		PublicIP     string `json:"public_ip"`
		PrivateIP    string `json:"private_ip"`
		// DeviceKey is sent on re-registration so the caller proves
		// possession of the machine's existing credential (C4).
		DeviceKey string `json:"device_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Re-registration of an existing machine: refresh its record and
	// return the original device (and key) so the agent keeps its
	// identity. Adopting an existing identity requires proof of key
	// possession or the enrollment secret (C4) - hostname+IP alone are
	// observable by anything on the network.
	if existing, err := h.store.Devices.GetByHostnameAndIP(req.Hostname, req.PrivateIP); err == nil {
		keyProven := req.DeviceKey != "" && req.DeviceKey == existing.DeviceKey
		if !keyProven && !h.validEnrollSecret(c) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "re-registration requires the device key or the enrollment secret"})
			return
		}
		existing.Name = req.Name
		existing.OS = req.OS
		existing.Arch = req.Arch
		existing.AgentVersion = req.AgentVersion
		existing.PublicIP = req.PublicIP
		existing.PrivateIP = req.PrivateIP
		existing.Status = "online"
		if err := h.store.Devices.Update(existing); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update device"})
			return
		}
		if err := h.store.Devices.UpdateLastSeen(existing.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update device"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"device":     existing,
			"device_key": existing.DeviceKey,
		})
		return
	}

	// First registration on this machine: if the install defines an
	// enrollment secret, new devices must present it (C4).
	if h.enrollSecret != "" && !h.validEnrollSecret(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid X-Enrollment-Secret header"})
		return
	}

	device := &models.Device{
		ID:           uuid.New().String(),
		Name:         req.Name,
		Hostname:     req.Hostname,
		OS:           req.OS,
		Arch:         req.Arch,
		AgentVersion: req.AgentVersion,
		Status:       "online",
		PublicIP:     req.PublicIP,
		PrivateIP:    req.PrivateIP,
		DeviceKey:    uuid.New().String(),
	}

	if err := h.store.Devices.Create(device); err != nil {
		// Concurrent install of the same machine: the lookup above lost
		// the race; return the winner instead of failing.
		if winner, werr := h.store.Devices.GetByHostnameAndIP(req.Hostname, req.PrivateIP); werr == nil {
			c.JSON(http.StatusOK, gin.H{"device": winner, "device_key": winner.DeviceKey})
			return
		}
		log.Printf("devices: register create failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register device"})
		return
	}

	events.Publish("device_registered", map[string]interface{}{
		"device_id": device.ID,
		"name":      device.Name,
		"hostname":  device.Hostname,
		"os":        device.OS,
	})

	c.JSON(http.StatusCreated, gin.H{
		"device":     device,
		"device_key": device.DeviceKey,
	})
}

// Heartbeat handles agent heartbeat updates.
func (h *DeviceHandler) Heartbeat(c *gin.Context) {
	deviceKey := c.GetHeader("X-Device-Key")
	if deviceKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing device key"})
		return
	}

	device, err := h.store.Devices.GetByKey(deviceKey)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid device key"})
		return
	}

	wasOffline := device.Status == "offline"
	if err := h.store.Devices.UpdateLastSeen(device.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update heartbeat"})
		return
	}

	// Broadcast heartbeat status
	h.hub.BroadcastMessage("heartbeat", map[string]interface{}{
		"device_id": device.ID,
		"device":    device.Name,
	})

	if wasOffline {
		events.Publish("device_online", map[string]interface{}{
			"device_id": device.ID,
			"name":      device.Name,
		})
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GetSelf returns this device's record to an agent that authenticated with
// its X-Device-Key. The agent uses it at startup to learn its own device
// ID, which it then uses to verify that deploy/scan/rollback payloads are
// addressed to it.
// GET /api/agent/me
func (h *DeviceHandler) GetSelf(c *gin.Context) {
	deviceKey := c.GetHeader("X-Device-Key")
	if deviceKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing device key"})
		return
	}

	device, err := h.store.Devices.GetByKey(deviceKey)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid device key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"device": device})
}

// ReportMetrics handles agent metrics reporting.
func (h *DeviceHandler) ReportMetrics(c *gin.Context) {
	deviceKey := c.GetHeader("X-Device-Key")
	if deviceKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing device key"})
		return
	}

	device, err := h.store.Devices.GetByKey(deviceKey)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid device key"})
		return
	}

	var metrics models.Metrics
	if err := c.ShouldBindJSON(&metrics); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update last seen
	if err := h.store.Devices.UpdateLastSeen(device.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update device"})
		return
	}

	// Persist metrics history
	if h.metricHistory != nil {
		hist := &models.MetricHistory{
			DeviceID:  device.ID,
			Timestamp: time.Now(),
			CPU:       metrics.CPU,
			RAM:       metrics.RAM,
			RAMUsed:   metrics.RAMUsed,
			RAMTotal:  metrics.RAMTotal,
			DiskUsage: metrics.DiskUsage,
			DiskUsed:  metrics.DiskUsed,
			DiskTotal: metrics.DiskTotal,
			NetIn:     metrics.NetIn,
			NetOut:    metrics.NetOut,
			Uptime:    metrics.Uptime,
			Processes: metrics.Processes,
		}
		// Fire-and-forget metric storage
		go func() {
			if err := h.metricHistory.Insert(hist); err != nil {
				log.Printf("failed to store metric history: %v", err)
			}
		}()
	}

	// Evaluate alerts
	h.alerts.Evaluate(metrics, device.ID, device.Name)

	// Broadcast metrics to users
	h.hub.BroadcastMessage("metrics", map[string]interface{}{
		"device_id": device.ID,
		"device":    device.Name,
		"metrics":   metrics,
	})

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ListDevices returns all devices.
func (h *DeviceHandler) ListDevices(c *gin.Context) {
	devices, err := h.store.Devices.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list devices"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"devices": devices})
}

// GetDevice returns a single device by ID.
func (h *DeviceHandler) GetDevice(c *gin.Context) {
	id := c.Param("id")
	device, err := h.store.Devices.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"device": device})
}

// DeleteDevice removes a device by ID.
func (h *DeviceHandler) DeleteDevice(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.Devices.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete device"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ClearMonitoringData deletes all stored metrics and alerts
// (Danger Zone: "clear all monitoring data"). Admin only (enforced by the
// route middleware).
// DELETE /api/monitoring/data
func (h *DeviceHandler) ClearMonitoringData(c *gin.Context) {
	metricsCleared, err := h.store.MetricHistory.ClearAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear metrics"})
		return
	}
	alertsCleared, err := h.store.Alerts.ClearAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear alerts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":          "ok",
		"metrics_cleared": metricsCleared,
		"alerts_cleared":  alertsCleared,
	})
}

// GetMetricsHistory returns historical metrics for a device.
// Query params: from (RFC3339 timestamp), to (RFC3339 timestamp, defaults to now)
func (h *DeviceHandler) GetMetricsHistory(c *gin.Context) {
	id := c.Param("id")
	if h.metricHistory == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "metric history store not configured"})
		return
	}

	from := time.Now().Add(-24 * time.Hour)
	to := time.Now()

	if fromStr := c.Query("from"); fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = parsed
		}
	}
	if toStr := c.Query("to"); toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = parsed
		}
	}

	metrics, err := h.metricHistory.QueryByDevice(id, from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"metrics": metrics})
}

// GetLatestMetrics returns the most recent metrics for a device.
func (h *DeviceHandler) GetLatestMetrics(c *gin.Context) {
	id := c.Param("id")
	if h.metricHistory == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "metric history store not configured"})
		return
	}

	metrics, err := h.metricHistory.QueryLatestByDevice(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"metrics": metrics})
}

// StartStream tells a device to switch to a shorter metrics-interval
// (H5: the device detail page wants 2 s streaming while it is open).
// The interval is clamped to 1..60 s server-side; the agent clamps
// again on receipt.
// POST /api/devices/:id/stream
func (h *DeviceHandler) StartStream(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Interval int `json:"interval"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
	}
	if req.Interval == 0 {
		req.Interval = 2 // default for an empty body
	}
	if req.Interval < 1 {
		req.Interval = 1
	}
	if req.Interval > 60 {
		req.Interval = 60
	}

	device, err := h.store.Devices.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	if err := h.hub.SendToDevice(device.DeviceKey, "stream", map[string]interface{}{
		"interval": req.Interval,
	}); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is not connected"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "streaming", "interval": req.Interval})
}

// StopStream tells a device to revert to its normal metrics interval.
// POST /api/devices/:id/stream/stop
func (h *DeviceHandler) StopStream(c *gin.Context) {
	id := c.Param("id")
	device, err := h.store.Devices.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}

	// Best effort: an offline device simply reverts on its next connect.
	if err := h.hub.SendToDevice(device.DeviceKey, "stream_end", map[string]interface{}{}); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is not connected"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "streaming_stopped"})
}
