package store

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/models"
)

// QueuedDeployStore persists deployments waiting for offline devices.
type QueuedDeployStore struct {
	db *gorm.DB
}

// Enqueue records (or extends) a queued deployment for a device/policy pair.
func (s *QueuedDeployStore) Enqueue(deviceID, policyID string, expires time.Time) error {
	var existing models.QueuedDeploy
	err := s.db.Where("device_id = ? AND policy_id = ?", deviceID, policyID).First(&existing).Error
	if err == nil {
		existing.ExpiresAt = expires
		return s.db.Save(&existing).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return s.db.Create(&models.QueuedDeploy{ID: uuid.NewString(), DeviceID: deviceID, PolicyID: policyID, ExpiresAt: expires}).Error
}

// ListActive returns entries that have not expired.
func (s *QueuedDeployStore) ListActive(now time.Time) ([]models.QueuedDeploy, error) {
	var out []models.QueuedDeploy
	if err := s.db.Where("expires_at > ?", now).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes one entry.
func (s *QueuedDeployStore) Delete(id string) error {
	return s.db.Delete(&models.QueuedDeploy{}, "id = ?", id).Error
}

// DeleteExpired removes entries past their expiry.
func (s *QueuedDeployStore) DeleteExpired(now time.Time) error {
	return s.db.Delete(&models.QueuedDeploy{}, "expires_at <= ?", now).Error
}
