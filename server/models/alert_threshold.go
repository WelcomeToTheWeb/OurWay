package models

import "time"

// The metric names a threshold rule can govern. The engine evaluates these and
// nothing else, so a rule naming another metric is refused at the API door rather
// than stored and silently never firing.
const (
	MetricCPU  = "cpu"
	MetricRAM  = "ram"
	MetricDisk = "disk"
)

// Threshold scope levels (2.4-5). Most specific wins, per metric, independently:
// a unit can take its cpu from a tag rule and its ram from its own rule.
//
// `global` is the installation floor and carries no scope value. `client` is a
// tenant, `tag` is a label across tenants, `device` is one unit.
const (
	ScopeGlobal = "global"
	ScopeClient = "client"
	ScopeTag    = "tag"
	ScopeDevice = "device"
)

// ScopeRank is the precedence of a scope type: the higher the number, the more
// specific, and the more specific rule wins.
func ScopeRank(scopeType string) int {
	switch scopeType {
	case ScopeDevice:
		return 4
	case ScopeTag:
		return 3
	case ScopeClient:
		return 2
	case ScopeGlobal:
		return 1
	default:
		return 0
	}
}

// ValidMetric is the metric vocabulary the engine evaluates.
func ValidMetric(metric string) bool {
	switch metric {
	case MetricCPU, MetricRAM, MetricDisk:
		return true
	default:
		return false
	}
}

// ValidScopeType is the scope vocabulary the resolver understands.
func ValidScopeType(scopeType string) bool {
	return ScopeRank(scopeType) > 0
}

// AlertThreshold is one threshold rule (2.4-5): a metric's warning and critical
// levels for a scope.
//
// The engine keeps its built-in defaults (cpu 80/90, ram 85/95, disk 85/95); a
// device with no applicable rule gets exactly those, byte for byte. A rule adds a
// more specific answer for the metric it names and nothing else.
type AlertThreshold struct {
	ID string `gorm:"type:uuid;primaryKey" json:"id"`

	// `ScopeValue` is empty for a global rule, a device id for a device rule, a
	// client id for a tenant rule, and a tag name for a tag rule.
	ScopeType  string `gorm:"index:idx_alert_threshold_scope;not null" json:"scope_type"`
	ScopeValue string `gorm:"index:idx_alert_threshold_scope;not null;default:''" json:"scope_value"`
	Metric     string `gorm:"index:idx_alert_threshold_scope;not null" json:"metric"`

	Warning  float64 `json:"warning"`
	Critical float64 `json:"critical"`

	// DedupWindowSeconds is per rule: a lab machine pinged every minute and a
	// remote unit that reports every ten minutes are not the same alerting
	// problem. 0 means the engine's default window.
	DedupWindowSeconds int `gorm:"not null;default:0" json:"dedup_window_seconds"`

	// No column default: GORM substitutes a column default for a zero-valued
	// field on insert, so `default:true` made a rule created with
	// `enabled: false` land **enabled**. The API states the state explicitly.
	Enabled   bool      `gorm:"not null" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ThresholdPair is a warning/critical pair, as the engine consumes it.
type ThresholdPair struct {
	Warning  float64 `json:"warning"`
	Critical float64 `json:"critical"`
}

// ResolvedThreshold is the threshold answer for one metric on one device, with
// the scope that produced it. "Why did this unit alert at 62?" is answered by the
// scope and value here, not by reading the whole rule list.
type ResolvedThreshold struct {
	Metric     string  `json:"metric"`
	Warning    float64 `json:"warning"`
	Critical   float64 `json:"critical"`
	ScopeType  string  `json:"scope_type"`
	ScopeValue string  `json:"scope_value"`
	// Source is "rule" or "built-in default": the operator's answer to "is my
	// rule in force on this unit, or am I looking at the shipped default?".
	Source       string `json:"source"`
	DedupSeconds int    `json:"dedup_window_seconds"`
}
