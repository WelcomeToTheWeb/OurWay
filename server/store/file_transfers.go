package store

import (
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// FileTransferStore provides CRUD operations for file transfers.
type FileTransferStore struct {
	db *gorm.DB
}

// NewFileTransferStore creates a new file transfer store.
func NewFileTransferStore(db *gorm.DB) *FileTransferStore {
	return &FileTransferStore{db: db}
}

// Create stores a new file transfer record.
func (s *FileTransferStore) Create(transfer *models.FileTransfer) error {
	return s.db.Create(transfer).Error
}

// GetByID retrieves a file transfer by ID.
func (s *FileTransferStore) GetByID(id string) (*models.FileTransfer, error) {
	var transfer models.FileTransfer
	err := s.db.First(&transfer, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &transfer, nil
}

// ListByDevice returns all file transfers for a device.
func (s *FileTransferStore) ListByDevice(deviceID string) ([]models.FileTransfer, error) {
	var transfers []models.FileTransfer
	err := s.db.Where("device_id = ?", deviceID).Order("created_at DESC").Find(&transfers).Error
	return transfers, err
}

// ListAll returns all file transfers, most recent first.
func (s *FileTransferStore) ListAll() ([]models.FileTransfer, error) {
	var transfers []models.FileTransfer
	err := s.db.Order("created_at DESC").Limit(100).Find(&transfers).Error
	return transfers, err
}

// UpdateStatus updates the status of a file transfer.
func (s *FileTransferStore) UpdateStatus(id string, status string, progress int, errMsg string) error {
	updates := map[string]interface{}{
		"status":        status,
		"progress":      progress,
		"error_message": errMsg,
		"updated_at":    time.Now(),
	}
	if status == "completed" || status == "failed" {
		now := time.Now()
		updates["completed_at"] = now
	}
	return s.db.Model(&models.FileTransfer{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateSize updates the file size of a transfer.
func (s *FileTransferStore) UpdateSize(id string, size int64) error {
	return s.db.Model(&models.FileTransfer{}).Where("id = ?", id).Update("size_bytes", size).Error
}
