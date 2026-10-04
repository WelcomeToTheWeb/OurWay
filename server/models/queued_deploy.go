package models

import "time"

// QueuedDeploy is a deployment waiting for an offline device to come back.
// PolicyID is "" for manual deployments (retried whenever the device
// returns, until ExpiresAt); policy entries are only retried inside the
// policy's maintenance window.
type QueuedDeploy struct {
	ID        string    `gorm:"primaryKey;type:text" json:"id"`
	DeviceID  string    `gorm:"not null;type:text;uniqueIndex:idx_queue_device_policy" json:"device_id"`
	PolicyID  string    `gorm:"not null;default:'';type:text;uniqueIndex:idx_queue_device_policy" json:"policy_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
}
