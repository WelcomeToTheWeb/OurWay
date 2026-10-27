package store

import (
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// DeviceStore provides CRUD operations for devices.
type DeviceStore struct {
	db *gorm.DB
}

// Create inserts a new device.
func (s *DeviceStore) Create(d *models.Device) error {
	return s.db.Create(d).Error
}

// GetByID fetches a device by its UUID.
func (s *DeviceStore) GetByID(id string) (*models.Device, error) {
	var d models.Device
	if err := s.db.First(&d, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// GetByKey fetches a device by its unique device key.
func (s *DeviceStore) GetByKey(key string) (*models.Device, error) {
	var d models.Device
	if err := s.db.First(&d, "device_key = ?", key).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// GetByHostnameAndIP finds an existing device with the same hostname and
// private IP — the stable identity used to make agent re-registration
// idempotent (re-running the installer must not create a duplicate device).
// If privateIP is empty, hostname alone is used.
func (s *DeviceStore) GetByHostnameAndIP(hostname, privateIP string) (*models.Device, error) {
	var d models.Device
	q := s.db.Where("hostname = ?", hostname)
	if privateIP != "" {
		q = q.Where("private_ip = ?", privateIP)
	}
	if err := q.Order("created_at ASC").First(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// ListAll returns every device.
func (s *DeviceStore) ListAll() ([]models.Device, error) {
	var devices []models.Device
	if err := s.db.Find(&devices).Error; err != nil {
		return nil, err
	}
	return devices, nil
}

// ListScoped returns the devices of the named clients, and the unassigned
// bucket when asked (2.3-0). The filter is in the WHERE clause: a scoped
// technician's list is a query over their clients, not a full read trimmed in
// the handler.
func (s *DeviceStore) ListScoped(clientIDs []string, includeUnassigned bool) ([]models.Device, error) {
	if len(clientIDs) == 0 && !includeUnassigned {
		// A caller with no scope sees nothing. Returning the fleet here would
		// turn "no rows granted" into "sees everything", which is the leak this
		// release exists to close.
		return []models.Device{}, nil
	}
	var devices []models.Device
	q := s.db.Model(&models.Device{})
	if includeUnassigned {
		q = q.Where("client_id IS NULL OR client_id IN ?", clientIDs)
	} else {
		q = q.Where("client_id IN ?", clientIDs)
	}
	if err := q.Find(&devices).Error; err != nil {
		return nil, err
	}
	return devices, nil
}

// Update persists changes to a device record.
func (s *DeviceStore) Update(d *models.Device) error {
	return s.db.Save(d).Error
}

// UpdateLastSeen updates the last-seen timestamp and status for a device.
func (s *DeviceStore) UpdateLastSeen(id string) error {
	return s.db.Model(&models.Device{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_seen": time.Now(),
			"status":    "online",
		}).Error
}

// UpdateCapabilities records what the agent reported about itself (2.2-1):
// whether remote input can land on it, and whether capture is producing
// frames. Empty values are left untouched, so an agent that reports only one
// field cannot erase the other, and an agent that reports nothing leaves the
// stored "unknown" in place.
func (s *DeviceStore) UpdateCapabilities(id, inputCapability, captureState string) error {
	updates := map[string]interface{}{}
	if inputCapability != "" {
		updates["input_capability"] = inputCapability
	}
	if captureState != "" {
		updates["capture_state"] = captureState
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.Model(&models.Device{}).Where("id = ?", id).Updates(updates).Error
}

// Delete removes a device by ID.
func (s *DeviceStore) Delete(id string) error {
	return s.db.Delete(&models.Device{}, "id = ?", id).Error
}
