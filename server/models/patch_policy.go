package models

import (
	"time"
)

// PatchPolicy defines the patch management policy for a scope of devices.
type PatchPolicy struct {
	ID                 string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name               string    `gorm:"not null;index" json:"name"`
	Scope              string    `gorm:"not null;default:all" json:"scope"` // all, tags, devices
	ScopeValue         string    `json:"scope_value"`
	Schedule           string    `gorm:"not null;default:weekly" json:"schedule"` // daily, weekly, monthly
	AutoReboot         bool      `gorm:"not null;default:false" json:"auto_reboot"`
	ApprovalRequired   bool      `gorm:"not null;default:true" json:"approval_required"`
	MaxDevicesPerBatch int       `gorm:"not null;default:10" json:"max_devices_per_batch"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
