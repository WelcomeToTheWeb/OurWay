package models

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Session represents an active remote control session between a user and a device.
//
// Frames travel over HTTP (POST /api/sessions/:id/frame) and input over
// the device WebSocket, so no WebRTC SDP/ICE state is stored here.
type Session struct {
	ID       string `gorm:"type:uuid;primaryKey" json:"id"`
	DeviceID string `gorm:"type:uuid;not null;index" json:"device_id"`
	UserID   string `gorm:"type:uuid;not null" json:"user_id"`
	Status   string `gorm:"not null;default:pending" json:"status"` // pending, active, ended, error
	// RemoteTokenHash is the SHA-256 (hex) of the per-session token handed
	// to the remote-control exe. It authenticates that exe for this session
	// only, so the device key never leaves the agent service.
	RemoteTokenHash string `gorm:"index" json:"-"`
	// ViewerTokenHash is the SHA-256 (hex) of the token handed to the
	// technician's native viewer (ourway:// launch URL). Like the
	// remote token it is scoped to this one session.
	ViewerTokenHash string     `gorm:"index" json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	EndedAt         *time.Time `json:"ended_at"`
}

// NewRemoteToken returns a fresh random per-session token and the hash
// to store for it.
func NewRemoteToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(b)
	return token, HashRemoteToken(token), nil
}

// HashRemoteToken returns the hex SHA-256 of a remote session token.
func HashRemoteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
