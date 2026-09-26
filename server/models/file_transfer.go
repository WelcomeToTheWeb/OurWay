package models

import (
	"time"
)

// FileTransfer tracks a file transfer operation between server and device.
type FileTransfer struct {
	ID          string     `gorm:"type:uuid;primaryKey" json:"id"`
	DeviceID    string     `gorm:"type:uuid;not null;index" json:"device_id"`
	Filename    string     `gorm:"not null" json:"filename"`
	Directory   string     `json:"directory"` // source or destination directory on device
	SourcePath  string     `json:"source_path"`   // local path on device (for pull)
	Destination string     `json:"destination"`   // remote path on device (for push)
	SizeBytes   int64      `json:"size_bytes"`
	Status      string     `gorm:"not null;default:pending" json:"status"` // pending, transferring, completed, failed
	Direction   string     `gorm:"not null;default:push" json:"direction"` // push (server->device), pull (device->server)
	Progress    int        `gorm:"not null;default:0" json:"progress"`     // 0-100
	ErrorMessage string    `json:"error_message"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at"`
}
