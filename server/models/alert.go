package models

import "time"

// Alert represents a threshold-based alert for a device.
type Alert struct {
	ID             string    `gorm:"type:uuid;primaryKey" json:"id"`
	DeviceID       string    `gorm:"type:uuid;index;not null" json:"device_id"`
	DeviceName     string    `gorm:"not null" json:"device_name"`
	Severity       string    `gorm:"not null" json:"severity"` // info, warning, critical
	Message        string    `gorm:"not null" json:"message"`
	Metric         string    `gorm:"not null" json:"metric"`
	Value          float64   `json:"value"`
	Threshold      float64   `json:"threshold"`
	Resolved       bool      `gorm:"default:false;index" json:"resolved"`
	Acknowledged   bool      `gorm:"default:false" json:"acknowledged"`
	AcknowledgedBy string    `gorm:"-" json:"acknowledged_by,omitempty"`
	AcknowledgedAt time.Time `json:"-"`
	AssignedTo     string    `gorm:"-" json:"assigned_to,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ResolvedAt     time.Time `json:"-"`
}
