package models

import "time"

// DeploymentResult records the per-device outcome of a patch deployment.
// The (deployment_id, device_id) pair is unique so that repeated result
// reports from an agent are idempotent: only a *changed* result moves the
// deployment counters.
type DeploymentResult struct {
	ID           string    `gorm:"type:uuid;primaryKey" json:"id"`
	DeploymentID string    `gorm:"type:uuid;not null;uniqueIndex:idx_deployment_device" json:"deployment_id"`
	DeviceID     string    `gorm:"type:uuid;not null;uniqueIndex:idx_deployment_device" json:"device_id"`
	Result       string    `gorm:"not null" json:"result"` // success, failed
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
