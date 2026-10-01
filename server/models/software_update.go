package models

import (
	"time"
)

// SoftwareUpdate represents a software update detected on a device.
type SoftwareUpdate struct {
	ID           string     `gorm:"type:uuid;primaryKey" json:"id"`
	DeviceID     string     `gorm:"type:uuid;not null;index" json:"device_id"`
	Source       string     `gorm:"not null" json:"source"`
	Title        string     `gorm:"not null" json:"title"`
	Version      string     `json:"version"`
	SizeBytes    int64      `json:"size_bytes"`
	Status       string     `gorm:"not null;default:detected" json:"status"` // detected, approved, downloading, installing, installed, failed, skipped
	InstalledAt  *time.Time `json:"installed_at"`
	ErrorMessage string     `json:"error_message"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
