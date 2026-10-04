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
			"status":       "installed",
			"installed_at": now,
		}).Error
}

// MarkFailed marks an update as failed with an error message.
func (s *SoftwareUpdateStore) MarkFailed(id string, errMsg string) error {
	return s.db.Model(&models.SoftwareUpdate{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        "failed",
			"error_message": errMsg,
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

// UpsertDetected records an update found by a scan without duplicating it.
// An update is the same when device, source and (external ID, else title
// and version) match. Existing rows keep their status (an approved update
// stays approved) except that a previously installed one is left alone.
// New rows start as "detected".
func (s *SoftwareUpdateStore) UpsertDetected(u *models.SoftwareUpdate) error {
	q := s.db.Where("device_id = ? AND source = ?", u.DeviceID, u.Source)
	if u.ExternalID != "" {
		q = q.Where("external_id = ?", u.ExternalID)
	} else {
		q = q.Where("title = ? AND version = ?", u.Title, u.Version)
	}
	var existing models.SoftwareUpdate
	err := q.First(&existing).Error
	if err == nil {
		existing.SizeBytes = u.SizeBytes
		existing.KB, existing.Severity, existing.Category = u.KB, u.Severity, u.Category
		if existing.Status == "installed" || existing.Status == "skipped" {
			// Still being reported as available: it came back (a new
			// release or a failed install). Offer it again.
			if existing.Status == "installed" {
				existing.Status = "detected"
			}
		}
		return s.db.Save(&existing).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	u.Status = "detected"
	return s.db.Create(u).Error
}

// MarkInstalling flags the updates as being installed by a deployment.
func (s *SoftwareUpdateStore) MarkInstalling(ids []string, deploymentID string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Model(&models.SoftwareUpdate{}).Where("id IN ?", ids).
		Updates(map[string]interface{}{"status": "installing", "deployment_id": deploymentID}).Error
}

// FinishDeployment resolves a device's installing updates for a
// deployment: installed on success, failed (with message) otherwise.
func (s *SoftwareUpdateStore) FinishDeployment(deploymentID, deviceID string, success bool, message string) error {
	changes := map[string]interface{}{"status": "installed", "installed_at": time.Now(), "error_message": ""}
	if !success {
		changes = map[string]interface{}{"status": "failed", "error_message": message}
	}
	return s.db.Model(&models.SoftwareUpdate{}).
		Where("deployment_id = ? AND device_id = ? AND status = ?", deploymentID, deviceID, "installing").
		Updates(changes).Error
}

// RevertToApproved returns a device's installing updates for a deployment
// to "approved" (the deploy command never reached the device, so nothing
// was attempted and they remain eligible for the next deployment).
func (s *SoftwareUpdateStore) RevertToApproved(deploymentID, deviceID string) error {
	return s.db.Model(&models.SoftwareUpdate{}).
		Where("deployment_id = ? AND device_id = ? AND status = ?", deploymentID, deviceID, "installing").
		Updates(map[string]interface{}{"status": "approved", "deployment_id": ""}).Error
}
