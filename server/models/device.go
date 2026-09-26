package models

import (
	"time"
)

// Device represents a managed device in the RMM system.
type Device struct {
	ID           string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name         string    `gorm:"not null;index" json:"name"`
	Hostname     string    `gorm:"not null" json:"hostname"`
	OS           string    `gorm:"not null" json:"os"`
	Arch         string    `gorm:"not null" json:"arch"`
	AgentVersion string    `json:"agent_version"`
	Status       string    `gorm:"not null;default:offline" json:"status"`
	LastSeen     time.Time `json:"last_seen"`
	PublicIP     string    `json:"public_ip"`
	PrivateIP    string    `json:"private_ip"`
	DeviceKey    string    `gorm:"uniqueIndex;not null" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
