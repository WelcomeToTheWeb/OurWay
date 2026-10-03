package models

import (
	"time"
)

// Session represents an active remote control session between a user and a device.
//
// Frames travel over HTTP (POST /api/sessions/:id/frame) and input over
// the device WebSocket, so no WebRTC SDP/ICE state is stored here.
type Session struct {
	ID        string     `gorm:"type:uuid;primaryKey" json:"id"`
	DeviceID  string     `gorm:"type:uuid;not null;index" json:"device_id"`
	UserID    string     `gorm:"type:uuid;not null" json:"user_id"`
	Status    string     `gorm:"not null;default:pending" json:"status"` // pending, active, ended, error
	CreatedAt time.Time  `json:"created_at"`
	EndedAt   *time.Time `json:"ended_at"`
}
