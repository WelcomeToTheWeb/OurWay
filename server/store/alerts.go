package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// AlertStore provides persistence for alerts.
type AlertStore struct {
	db *gorm.DB
}

// Create inserts a new alert.
func (s *AlertStore) Create(a *models.Alert) error {
	return s.db.Create(a).Error
}

// ListScoped returns alerts whose **unit** is in `deviceIDs` — the caller's
// visible fleet. An empty set answers no rows: a caller with no scope has no
// alerts, and reading the whole table to trim it in the handler is both the leak
// and the cost this release is meant to remove (2.3-2's criterion is that the
// client filter is in the SQL).
//
// `deviceIDs` is the unit set, not a client set: alerts store a `device_id` and
// no client, so the client meaning is resolved once, by the caller, into units.
func (s *AlertStore) ListScoped(deviceIDs []string, deviceID *string, unresolvedOnly bool) ([]models.Alert, error) {
	if len(deviceIDs) == 0 {
		return []models.Alert{}, nil
	}
	query := s.db.Order("created_at DESC").Where("device_id IN ?", deviceIDs)
	if deviceID != nil && *deviceID != "" {
		query = query.Where("device_id = ?", *deviceID)
	}
	if unresolvedOnly {
		query = query.Where("resolved = ?", false)
	}
	var alerts []models.Alert
	if err := query.Find(&alerts).Error; err != nil {
		return nil, err
	}
	return alerts, nil
}

// List returns alerts, optionally filtered by device or resolved status.
func (s *AlertStore) List(deviceID *string, unresolvedOnly bool) ([]models.Alert, error) {
	query := s.db.Order("created_at DESC")
	if deviceID != nil && *deviceID != "" {
		query = query.Where("device_id = ?", *deviceID)
	}
	if unresolvedOnly {
		query = query.Where("resolved = ?", false)
	}
	var alerts []models.Alert
	if err := query.Find(&alerts).Error; err != nil {
		return nil, err
	}
	return alerts, nil
}

// GetByID retrieves an alert by its ID.
func (s *AlertStore) GetByID(id string) (*models.Alert, error) {
	var alert models.Alert
	if err := s.db.First(&alert, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &alert, nil
}

// MarkResolved sets an alert's resolved flag to true.
func (s *AlertStore) MarkResolved(id string) error {
	return s.db.Model(&models.Alert{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"resolved":    true,
			"resolved_at": time.Now(),
		}).Error
}

// Acknowledge marks an alert as acknowledged by a user.
func (s *AlertStore) Acknowledge(id, userID string) error {
	return s.db.Model(&models.Alert{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"acknowledged":    true,
			"acknowledged_by": userID,
			"acknowledged_at": time.Now(),
		}).Error
}

// Assign assigns an alert to a user.
func (s *AlertStore) Assign(id, userID string) error {
	return s.db.Model(&models.Alert{}).
		Where("id = ?", id).
		Update("assigned_to", userID).Error
}

// ClearAll deletes every stored alert (Danger Zone: clear monitoring data).
func (s *AlertStore) ClearAll() (int64, error) {
	result := s.db.Delete(&models.Alert{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to clear alerts: %w", result.Error)
	}
	return result.RowsAffected, nil
}
