package alerts

import (
	"fmt"
	"log"
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
	alertStore   store.AlertStoreInterface
	mu           sync.RWMutex
	thresholds   map[string]Thresholds
	dedupWindow  time.Duration
	recentAlerts map[string]time.Time // key: deviceID:metric:severity -> last alert time

	// 2.4-5: the rule set, and the scope keys a device is measured against.
	// Both are optional: with no rule store the engine answers its built-in
	// defaults, which is the pre-2.4.5 behaviour exactly.
	rules      ThresholdSource
	scopes     ScopeResolver
	scopeCache map[string]scopeEntry
}

// ThresholdSource answers the thresholds for one device and metric (2.4-5). The
// store implements it with device → tag → client → global precedence and a cached
// snapshot, so measuring a metric costs no database round trip.
type ThresholdSource interface {
	Resolve(deviceID string, tags []string, clientID, metric string) (*models.ResolvedThreshold, error)
}

// DeviceScope is the scope a device's metrics are measured against: the tags on
// the device row, and the tenant it belongs to.
type DeviceScope struct {
	DeviceID string
	Tags     []string
	ClientID string
}

// ScopeResolver answers a device's scope keys. The API wires it from the device
// store; the engine caches the answer briefly, because a fleet posts metrics
// continuously and tags do not move that fast.
type ScopeResolver func(deviceID string) (DeviceScope, error)

// WireDeviceScopes connects the engine to the store's rule set and to a device's
// scope keys (2.4-5). `main.go` and the test harness call this same function, so a
// test exercises the wiring production runs with.
func WireDeviceScopes(e *Engine, st *store.Store) {
	e.SetThresholdSource(st.Thresholds, func(deviceID string) (DeviceScope, error) {
		device, err := st.Devices.GetByID(deviceID)
		if err != nil {
			return DeviceScope{}, err
		}
		clientID := ""
		if device.ClientID != nil {
			clientID = *device.ClientID
		}
		return DeviceScope{DeviceID: deviceID, Tags: device.Tags, ClientID: clientID}, nil
	})
}

// scopeTTL is how long a cached device scope stays authoritative. A tag change is
// visible within this window; a metric post costs one read per window per device.
const scopeTTL = 60 * time.Second

type scopeEntry struct {
	scope   DeviceScope
	fetched time.Time
}

// NewEngine creates a new alert engine with default thresholds.
func NewEngine(alertStore store.AlertStoreInterface) *Engine {
	return &Engine{
		alertStore: alertStore,
		// The built-in floor. A rule is more specific than these numbers and
		// wins; with no rule the device gets exactly these, as `v2.3.x` gave
		// them (the byte-identical case the 2.4-5 criterion names).
		thresholds: map[string]Thresholds{
			"cpu":  {Warning: 80, Critical: 90},
			"ram":  {Warning: 85, Critical: 95},
			"disk": {Warning: 85, Critical: 95},
		},
		dedupWindow:  5 * time.Minute,
		recentAlerts: make(map[string]time.Time),
		scopeCache:   make(map[string]scopeEntry),
	}
}

// SetThresholdSource wires the threshold rules and the device scope lookup
// (2.4-5). Passing nil for either restores the built-in behaviour.
func (e *Engine) SetThresholdSource(rules ThresholdSource, scopes ScopeResolver) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = rules
	e.scopes = scopes
	e.scopeCache = make(map[string]scopeEntry)
}

// SetThresholds updates the thresholds for a metric.
func (e *Engine) SetThresholds(metric string, t Thresholds) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.thresholds[metric] = t
}

// SetDedupWindow sets the alert deduplication window.
func (e *Engine) SetDedupWindow(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dedupWindow = d
}

// Evaluate checks metrics against thresholds, creates alerts as needed,
// and auto-resolves alerts whose metric has returned to normal.
//
// The thresholds are resolved per metric and per device (2.4-5): device → tag →
// client → global, most specific wins, each metric independently. A device with no
// applicable rule is measured against exactly the built-in numbers.
func (e *Engine) Evaluate(m models.Metrics, deviceID, deviceName string) {
	scope := e.deviceScope(deviceID)
	e.checkThreshold(scope, "cpu", m.CPU, deviceID, deviceName, "CPU usage")
	e.checkThreshold(scope, "ram", m.RAM, deviceID, deviceName, "RAM usage")
	e.checkThreshold(scope, "disk", m.DiskUsage, deviceID, deviceName, "Disk usage")
	e.autoResolve(m, scope)
}

// deviceScope answers the scope keys a device is measured against, from the wired
// resolver, cached for `scopeTTL` (a fleet posts metrics continuously; tags and
// tenancy do not move that fast).
func (e *Engine) deviceScope(deviceID string) DeviceScope {
	e.mu.RLock()
	resolver, cached := e.scopes, e.scopeCache[deviceID]
	e.mu.RUnlock()

	fallback := DeviceScope{DeviceID: deviceID}
	if resolver == nil {
		return fallback
	}
	if time.Since(cached.fetched) < scopeTTL {
		return cached.scope
	}
	scope, err := resolver(deviceID)
	if err != nil {
		// A device whose scope cannot be read is measured against the global
		// floor, not skipped: losing the metric evaluation is worse than
		// evaluating it against the installation defaults.
		return fallback
	}
	scope.DeviceID = deviceID
	e.mu.Lock()
	if e.scopeCache == nil {
		e.scopeCache = make(map[string]scopeEntry)
	}
	e.scopeCache[deviceID] = scopeEntry{scope: scope, fetched: time.Now()}
	e.mu.Unlock()
	return scope
}

// resolve answers the thresholds for one metric on one device: the most specific
// enabled rule that applies, falling back to the engine's built-in floor.
func (e *Engine) resolve(scope DeviceScope, metric string) models.ResolvedThreshold {
	e.mu.RLock()
	rules := e.rules
	floor := e.thresholds[metric]
	hasFloor := okThreshold(e.thresholds, metric)
	e.mu.RUnlock()

	if rules != nil {
		if resolved, err := rules.Resolve(scope.DeviceID, scope.Tags, scope.ClientID, metric); err == nil && resolved != nil {
			return *resolved
		}
		// A rule store that cannot answer is not a reason to drop the metric.
	}
	if !hasFloor {
		return models.ResolvedThreshold{Metric: metric}
	}
	return models.ResolvedThreshold{
		Metric:    metric,
		Warning:   floor.Warning,
		Critical:  floor.Critical,
		ScopeType: models.ScopeGlobal,
		Source:    "built-in default",
	}
}

func okThreshold(m map[string]Thresholds, metric string) bool {
	_, ok := m[metric]
	return ok
}

// autoResolve marks unresolved alerts as resolved when their metric has
// dropped back to or below the warning threshold, so a recovering device
// does not keep stale alerts open.
func (e *Engine) autoResolve(m models.Metrics, scope DeviceScope) {
	// The **resolved** warning, not the global one (2.4-5): a unit whose rule
	// says 50 is recovering at 48. Measuring it against the installation's 80
	// would leave an alert open that the operator's own rule says is over.
	normal := map[string]bool{}
	values := map[string]float64{
		"cpu":  m.CPU,
		"ram":  m.RAM,
		"disk": m.DiskUsage,
	}
	for _, metric := range []string{"cpu", "ram", "disk"} {
		resolved := e.resolve(scope, metric)
		if resolved.Critical == 0 && resolved.Warning == 0 {
			continue
		}
		if values[metric] <= resolved.Warning {
			normal[metric] = true
		}
	}

	if len(normal) == 0 {
		return
	}
	deviceID := scope.DeviceID

	alerts, err := e.alertStore.List(&deviceID, true)
	if err != nil {
		return
	}

	for i := range alerts {
		a := &alerts[i]
		if !normal[a.Metric] {
			continue
		}
		if err := e.alertStore.MarkResolved(a.ID); err != nil {
			continue
		}
		events.Publish("alert_resolved", map[string]interface{}{
			"alert_id":  a.ID,
			"device_id": deviceID,
			"device":    a.DeviceName,
			"metric":    a.Metric,
			"auto":      true,
		})
	}
}

// checkThreshold evaluates a single metric against its thresholds.
func (e *Engine) checkThreshold(scope DeviceScope, metric string, value float64, deviceID, deviceName, label string) {
	resolved := e.resolve(scope, metric)
	t := Thresholds{Warning: resolved.Warning, Critical: resolved.Critical}
	if t.Critical == 0 && t.Warning == 0 {
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

	// Deduplication: don't fire same alert within window. The window comes from
	// the rule that fired (2.4-5) — a lab unit reporting every minute and a
	// remote unit reporting every ten minutes are not one alerting problem.
	key := fmt.Sprintf("%s:%s:%s", deviceID, metric, severity)
	now := time.Now()
	window := e.dedupWindow
	if resolved.DedupSeconds > 0 {
		window = time.Duration(resolved.DedupSeconds) * time.Second
	}

	e.mu.Lock()
	// Clean up old entries
	for k, v := range e.recentAlerts {
		if now.Sub(v) > window {
			delete(e.recentAlerts, k)
		}
	}

	if lastAlert, exists := e.recentAlerts[key]; exists {
		if now.Sub(lastAlert) < window {
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
		// The alert was not persisted; do not publish an event for an
		// alert that does not exist.
		log.Printf("alerts: failed to create alert %s: %v", alert.ID, err)
		return
	}

	events.Publish("alert_created", map[string]interface{}{
		"alert_id":  alert.ID,
		"device_id": deviceID,
		"device":    deviceName,
		"severity":  severity,
		"message":   message,
		"metric":    metric,
		"value":     value,
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
