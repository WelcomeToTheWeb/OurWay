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

// Delete removes a device by ID.
func (s *DeviceStore) Delete(id string) error {
	return s.db.Delete(&models.Device{}, "id = ?", id).Error
}
