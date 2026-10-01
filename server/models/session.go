package models

import (
	"time"

	"gorm.io/datatypes"
)

// Session represents an active remote control session between a user and a device.
type Session struct {
	ID            string         `gorm:"type:uuid;primaryKey" json:"id"`
	DeviceID      string         `gorm:"type:uuid;not null;index" json:"device_id"`
	UserID        string         `gorm:"type:uuid;not null" json:"user_id"`
	Status        string         `gorm:"not null;default:pending" json:"status"` // pending, active, ended, error
	OfferSDP      string         `json:"offer_sdp"`
	AnswerSDP     string         `json:"answer_sdp"`
	IceCandidates datatypes.JSON `json:"ice_candidates"`
	CreatedAt     time.Time      `json:"created_at"`
	EndedAt       *time.Time     `json:"ended_at"`
}
