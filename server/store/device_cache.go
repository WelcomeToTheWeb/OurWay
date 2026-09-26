package store

import (
	"time"

	"ourway/server/cache"
	"ourway/server/models"
)

// CachedDeviceStore wraps DeviceStore with Redis caching for device status lookups.
type CachedDeviceStore struct {
	ds    DeviceStoreInterface
	cache *cache.RedisClient
}

// NewCachedDeviceStore creates a cached device store.
func NewCachedDeviceStore(ds DeviceStoreInterface, rc *cache.RedisClient) *CachedDeviceStore {
	return &CachedDeviceStore{
		ds:    ds,
		cache: rc,
	}
}

// Create inserts a new device.
func (s *CachedDeviceStore) Create(d *models.Device) error {
	err := s.ds.Create(d)
	if err == nil && s.cache != nil {
		s.cache.Delete("device:" + d.ID)
	}
	return err
}

// GetByID retrieves a device by ID, using cache if available.
func (s *CachedDeviceStore) GetByID(id string) (*models.Device, error) {
	if s.cache != nil {
		var cached models.Device
		if err := s.cache.Get("device:"+id, &cached); err == nil {
			return &cached, nil
		}
	}

	device, err := s.ds.GetByID(id)
	if err != nil {
		return nil, err
	}

	if s.cache != nil {
		s.cache.Set("device:"+id, device, 30*time.Second)
	}

	return device, nil
}

// GetByKey fetches a device by its unique device key.
func (s *CachedDeviceStore) GetByKey(key string) (*models.Device, error) {
	return s.ds.GetByKey(key)
}

// ListAll returns every device.
func (s *CachedDeviceStore) ListAll() ([]models.Device, error) {
	return s.ds.ListAll()
}

// Update updates a device and invalidates its cache.
func (s *CachedDeviceStore) Update(device *models.Device) error {
	err := s.ds.Update(device)
	if err == nil && s.cache != nil {
		s.cache.Delete("device:" + device.ID)
	}
	return err
}

// UpdateLastSeen updates the last-seen timestamp and status for a device.
func (s *CachedDeviceStore) UpdateLastSeen(id string) error {
	err := s.ds.UpdateLastSeen(id)
	if err == nil && s.cache != nil {
		s.cache.Delete("device:" + id)
	}
	return err
}

// Delete removes a device by ID.
func (s *CachedDeviceStore) Delete(id string) error {
	err := s.ds.Delete(id)
	if err == nil && s.cache != nil {
		s.cache.Delete("device:" + id)
	}
	return err
}
