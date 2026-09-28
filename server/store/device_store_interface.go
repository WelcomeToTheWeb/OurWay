package store

import (
	"ourway/server/models"
)

// DeviceStoreInterface defines the interface for device storage operations.
type DeviceStoreInterface interface {
	Create(d *models.Device) error
	GetByID(id string) (*models.Device, error)
	GetByKey(key string) (*models.Device, error)
	GetByHostnameAndIP(hostname, privateIP string) (*models.Device, error)
	ListAll() ([]models.Device, error)
	Update(d *models.Device) error
	UpdateLastSeen(id string) error
	Delete(id string) error
}
