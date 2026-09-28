package models

import (
	"time"
)

// PatchDeployment tracks a patch deployment operation.
type PatchDeployment struct {
	ID             string     `gorm:"type:uuid;primaryKey" json:"id"`
	PolicyID       string     `gorm:"type:uuid;not null;index" json:"policy_id"`
	Status         string     `gorm:"not null;default:pending" json:"status"` // pending, running, completed, failed
	DeviceIDs      []string   `gorm:"type:text;serializer:json" json:"device_ids"`
	DevicesTotal   int        `json:"devices_total"`
	DevicesSuccess int        `json:"devices_success"`
	DevicesFailed  int        `json:"devices_failed"`
	StartedAt      *time.Time `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	TimeoutAt      *time.Time `json:"timeout_at"`
	Message        string     `json:"message"`
	CreatedAt      time.Time  `json:"created_at"`
}
