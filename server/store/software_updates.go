package store

import (
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// SoftwareUpdateStore provides CRUD operations for software updates.
type SoftwareUpdateStore struct {
	db *gorm.DB
}

// Create inserts a new software update record.
func (s *SoftwareUpdateStore) Create(update *models.SoftwareUpdate) error {
	return s.db.Create(update).Error
}

// GetByID fetches a software update by ID.
func (s *SoftwareUpdateStore) GetByID(id string) (*models.SoftwareUpdate, error) {
	var update models.SoftwareUpdate
	if err := s.db.First(&update, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &update, nil
}

// ListByDevice returns all software updates for a device.
func (s *SoftwareUpdateStore) ListByDevice(deviceID string) ([]models.SoftwareUpdate, error) {
	var updates []models.SoftwareUpdate
	if err := s.db.Where("device_id = ?", deviceID).Find(&updates).Error; err != nil {
		return nil, err
	}
	return updates, nil
}

// ListByStatus returns all software updates with a given status.
func (s *SoftwareUpdateStore) ListByStatus(status string) ([]models.SoftwareUpdate, error) {
	var updates []models.SoftwareUpdate
	if err := s.db.Where("status = ?", status).Find(&updates).Error; err != nil {
		return nil, err
	}
	return updates, nil
}

// ListByDeviceAndStatus returns updates for a device with a given status.
func (s *SoftwareUpdateStore) ListByDeviceAndStatus(deviceID, status string) ([]models.SoftwareUpdate, error) {
	var updates []models.SoftwareUpdate
	if err := s.db.Where("device_id = ? AND status = ?", deviceID, status).Find(&updates).Error; err != nil {
		return nil, err
	}
	return updates, nil
}

// MarkApproved transitions an update from "detected" to "approved".
// Returns the number of rows affected (0 if the update was not in "detected" state).
func (s *SoftwareUpdateStore) MarkApproved(id string) (int64, error) {
	res := s.db.Model(&models.SoftwareUpdate{}).
		Where("id = ? AND status = 'detected'", id).
		Updates(map[string]interface{}{
			"status":     "approved",
			"updated_at": time.Now(),
		})
	return res.RowsAffected, res.Error
}

// Update persists changes to a software update.
func (s *SoftwareUpdateStore) Update(update *models.SoftwareUpdate) error {
	return s.db.Save(update).Error
}

// MarkInstalled marks an update as installed.
func (s *SoftwareUpdateStore) MarkInstalled(id string) error {
	now := time.Now()
	return s.db.Model(&models.SoftwareUpdate{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":      "installed",
			"installed_at": now,
		}).Error
}

// MarkFailed marks an update as failed with an error message.
func (s *SoftwareUpdateStore) MarkFailed(id string, errMsg string) error {
	return s.db.Model(&models.SoftwareUpdate{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":         "failed",
			"error_message":  errMsg,
		}).Error
}

// CountByDeviceAndStatus counts updates for a device by status.
func (s *SoftwareUpdateStore) CountByDeviceAndStatus(deviceID, status string) (int64, error) {
	var count int64
	err := s.db.Model(&models.SoftwareUpdate{}).
		Where("device_id = ? AND status = ?", deviceID, status).
		Count(&count).Error
	return count, err
}
