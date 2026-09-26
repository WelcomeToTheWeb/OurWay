package models

import (
	"time"
)

// MetricHistory stores a single metrics snapshot for a device at a point in time.
// Designed for time-series storage with TimescaleDB or plain PostgreSQL.
type MetricHistory struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID  string    `gorm:"type:uuid;not null;index:idx_metric_device_time,priority:1" json:"device_id"`
	Timestamp time.Time `gorm:"not null;index:idx_metric_device_time,priority:2;default:now()" json:"timestamp"`
	CPU       float64   `json:"cpu"`
	RAM       float64   `json:"ram"`
	RAMUsed   uint64    `json:"ram_used"`
	RAMTotal  uint64    `json:"ram_total"`
	DiskUsage float64   `json:"disk_usage"`
	DiskUsed  uint64    `json:"disk_used"`
	DiskTotal uint64    `json:"disk_total"`
	NetIn     uint64    `json:"net_in"`
	NetOut    uint64    `json:"net_out"`
	Uptime    uint64    `json:"uptime"`
	Processes int       `json:"processes"`
}

// TableName specifies the table name for the model.
func (MetricHistory) TableName() string {
	return "metric_history"
}
