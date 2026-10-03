package models

import "time"

// DeploymentResult records the per-device outcome of a patch deployment.
// The (deployment_id, device_id) pair is unique so that repeated result
// reports from an agent are idempotent: only a *changed* result moves the
// deployment counters.
//
// Kind distinguishes the phase that produced the outcome ("deploy" or
// "rollback") so rollback results — including failures — are recorded and
// visible instead of being overwritten invisibly (M14). Message carries the
// agent's human-readable detail (e.g. why a rollback was refused).
type DeploymentResult struct {
	ID           string    `gorm:"type:uuid;primaryKey" json:"id"`
	DeploymentID string    `gorm:"type:uuid;not null;uniqueIndex:idx_deployment_device" json:"deployment_id"`
	DeviceID     string    `gorm:"type:uuid;not null;uniqueIndex:idx_deployment_device" json:"device_id"`
	Result       string    `gorm:"not null" json:"result"`                // success, failed
	Kind         string    `gorm:"not null;default:'deploy'" json:"kind"` // deploy, rollback
	Message      string    `json:"message"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
