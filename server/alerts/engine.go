package alerts

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"ourway/server/events"
	"ourway/server/models"
	"ourway/server/store"
)

// Thresholds defines warning and critical thresholds for a metric.
type Thresholds struct {
	Warning  float64
	Critical float64
}

// Engine evaluates metrics against thresholds and creates alerts.
type Engine struct {
	alertStore store.AlertStoreInterface
	mu         sync.RWMutex
	thresholds map[string]Thresholds
	dedupWindow time.Duration
	recentAlerts map[string]time.Time // key: deviceID:metric:severity -> last alert time
}

// NewEngine creates a new alert engine with default thresholds.
func NewEngine(alertStore store.AlertStoreInterface) *Engine {
	return &Engine{
		alertStore: alertStore,
		thresholds: map[string]Thresholds{
			"cpu":  {Warning: 80, Critical: 90},
			"ram":  {Warning: 85, Critical: 95},
			"disk": {Warning: 85, Critical: 95},
		},
		dedupWindow: 5 * time.Minute,
		recentAlerts: make(map[string]time.Time),
	}
}

// SetThresholds updates the thresholds for a metric.
func (e *Engine) SetThresholds(metric string, t Thresholds) {
	e.thresholds[metric] = t
}

// SetDedupWindow sets the alert deduplication window.
func (e *Engine) SetDedupWindow(d time.Duration) {
	e.dedupWindow = d
}

// Evaluate checks metrics against thresholds and creates alerts as needed.
func (e *Engine) Evaluate(m models.Metrics, deviceID, deviceName string) {
	e.checkThreshold("cpu", m.CPU, deviceID, deviceName, "CPU usage")
	e.checkThreshold("ram", m.RAM, deviceID, deviceName, "RAM usage")
	e.checkThreshold("disk", m.DiskUsage, deviceID, deviceName, "Disk usage")
}

// checkThreshold evaluates a single metric against its thresholds.
func (e *Engine) checkThreshold(metric string, value float64, deviceID, deviceName, label string) {
	e.mu.RLock()
	t, ok := e.thresholds[metric]
	e.mu.RUnlock()

	if !ok {
		return
	}

	severity := ""
	threshold := 0.0
	message := ""

	if value > t.Critical {
		severity = "critical"
		threshold = t.Critical
		message = fmt.Sprintf("%s high: %.1f%% (threshold: %.0f%%)", label, value, threshold)
	} else if value > t.Warning {
		severity = "warning"
		threshold = t.Warning
		message = fmt.Sprintf("%s elevated: %.1f%% (threshold: %.0f%%)", label, value, threshold)
	} else {
		return // Below warning threshold
	}

	// Deduplication: don't fire same alert within window
	key := fmt.Sprintf("%s:%s:%s", deviceID, metric, severity)
	now := time.Now()

	e.mu.Lock()
	// Clean up old entries
	for k, v := range e.recentAlerts {
		if now.Sub(v) > e.dedupWindow {
			delete(e.recentAlerts, k)
		}
	}

	if lastAlert, exists := e.recentAlerts[key]; exists {
		if now.Sub(lastAlert) < e.dedupWindow {
			e.mu.Unlock()
			return // Within dedup window, skip
		}
	}

	e.recentAlerts[key] = now
	e.mu.Unlock()

	alert := &models.Alert{
		ID:         uuid.New().String(),
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Severity:   severity,
		Message:    message,
		Metric:     metric,
		Value:      value,
		Threshold:  threshold,
		Resolved:   false,
		CreatedAt:  now,
	}

	if err := e.alertStore.Create(alert); err != nil {
		// Log but don't fail
		_ = err
	}

	events.Publish("alert_created", map[string]interface{}{
		"alert_id":   alert.ID,
		"device_id":  deviceID,
		"device":     deviceName,
		"severity":   severity,
		"message":    message,
		"metric":     metric,
		"value":      value,
	})
}

// AcknowledgeAlert marks an alert as acknowledged.
func (e *Engine) AcknowledgeAlert(id, userID string) error {
	return e.alertStore.Acknowledge(id, userID)
}

// AssignAlert assigns an alert to a user.
func (e *Engine) AssignAlert(id, userID string) error {
	return e.alertStore.Assign(id, userID)
}
