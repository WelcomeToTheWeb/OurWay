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
	// RebootPending is set when a deployment reported that the device
	// needs a reboot to finish patching; cleared when a reboot is sent.
	RebootPending bool `gorm:"not null;default:false" json:"reboot_pending"`
	// DeviceKey is the agent's wire credential. It is never serialized
	// in JSON (C3): any API that exposes it lets the holder connect as
	// the device. It is returned explicitly, exactly once, by
	// POST /api/agent/register.
	DeviceKey string    `gorm:"uniqueIndex;not null" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
