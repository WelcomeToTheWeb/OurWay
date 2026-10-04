package models

import (
	"time"
)

// PatchPolicy defines the patch management policy for a scope of devices.
type PatchPolicy struct {
	ID         string `gorm:"type:uuid;primaryKey" json:"id"`
	Name       string `gorm:"not null;index" json:"name"`
	Scope      string `gorm:"not null;default:all" json:"scope"` // all, tags, devices
	ScopeValue string `json:"scope_value"`
	Schedule   string `gorm:"not null;default:weekly" json:"schedule"` // daily, weekly, monthly
	// AutoReboot/ApprovalRequired carry no gorm default tag on purpose: a
	// default tag makes GORM substitute the column default for the struct's
	// zero value on create, so approval_required=false could never be
	// stored. The API layer applies the safe defaults instead.
	AutoReboot         bool `gorm:"not null" json:"auto_reboot"`
	ApprovalRequired   bool `gorm:"not null" json:"approval_required"`
	MaxDevicesPerBatch int  `gorm:"not null;default:10" json:"max_devices_per_batch"`
	// Maintenance window: the policy runs once per scheduled day, starting at
	// WindowStart ("HH:MM" in Timezone) and lasting WindowHours. Empty values
	// mean the whole scheduled day in UTC (how policies behaved before
	// windows existed).
	WindowStart string     `json:"window_start"`
	WindowHours int        `json:"window_hours"`
	Timezone    string     `json:"timezone"`
	LastRunAt   *time.Time `json:"last_run_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
