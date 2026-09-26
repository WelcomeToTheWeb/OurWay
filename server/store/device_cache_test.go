package store

import (
	"fmt"
	"testing"
	"time"

	"ourway/server/models"
)

// MockDeviceStore is a mock implementation of DeviceStoreInterface for testing
type MockDeviceStore struct {
	devices map[string]*models.Device
}

func NewMockDeviceStore() *MockDeviceStore {
	return &MockDeviceStore{
		devices: make(map[string]*models.Device),
	}
}

func (m *MockDeviceStore) Create(d *models.Device) error {
	m.devices[d.ID] = d
	return nil
}

func (m *MockDeviceStore) GetByID(id string) (*models.Device, error) {
	d, ok := m.devices[id]
	if !ok {
		return nil, fmt.Errorf("device not found: %s", id)
	}
	return d, nil
}

func (m *MockDeviceStore) GetByKey(key string) (*models.Device, error) {
	for _, d := range m.devices {
		if d.DeviceKey == key {
			return d, nil
		}
	}
	return nil, nil
}

func (m *MockDeviceStore) ListAll() ([]models.Device, error) {
	var result []models.Device
	for _, d := range m.devices {
		result = append(result, *d)
	}
	return result, nil
}

func (m *MockDeviceStore) Update(d *models.Device) error {
	m.devices[d.ID] = d
	return nil
}

func (m *MockDeviceStore) UpdateLastSeen(id string) error {
	if d, ok := m.devices[id]; ok {
		d.LastSeen = time.Now()
		d.Status = "online"
	}
	return nil
}

func (m *MockDeviceStore) Delete(id string) error {
	delete(m.devices, id)
	return nil
}

// Note: These tests don't require Redis - they test the caching logic
// with a mock store. The actual Redis caching requires a running instance.

func TestCachedDeviceStoreCreate(t *testing.T) {
	mock := NewMockDeviceStore()
	cached := NewCachedDeviceStore(mock, nil) // nil cache

	device := &models.Device{
		ID:        "test-device-1",
		Name:      "Test Device",
		Hostname:  "test.local",
		OS:        "linux",
		Arch:      "amd64",
		DeviceKey: "test-key-1",
		Status:    "online",
	}

	err := cached.Create(device)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	stored, err := mock.GetByID("test-device-1")
	if err != nil || stored == nil {
		t.Fatal("Device not stored")
	}
	if stored.Name != "Test Device" {
		t.Errorf("Expected name 'Test Device', got '%s'", stored.Name)
	}
}

func TestCachedDeviceStoreGetByID(t *testing.T) {
	mock := NewMockDeviceStore()
	cached := NewCachedDeviceStore(mock, nil)

	device := &models.Device{
		ID:        "test-device-2",
		Name:      "Test Device 2",
		Hostname:  "test2.local",
		OS:        "linux",
		Arch:      "amd64",
		DeviceKey: "test-key-2",
		Status:    "online",
	}

	err := mock.Create(device)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := cached.GetByID("test-device-2")
	if err != nil || got == nil {
		t.Fatal("GetByID failed")
	}
	if got.Name != "Test Device 2" {
		t.Errorf("Expected name 'Test Device 2', got '%s'", got.Name)
	}
}

func TestCachedDeviceStoreUpdate(t *testing.T) {
	mock := NewMockDeviceStore()
	cached := NewCachedDeviceStore(mock, nil)

	device := &models.Device{
		ID:        "test-device-3",
		Name:      "Test Device 3",
		Hostname:  "test3.local",
		OS:        "linux",
		Arch:      "amd64",
		DeviceKey: "test-key-3",
		Status:    "online",
	}

	err := mock.Create(device)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	device.Name = "Updated Device 3"
	err = cached.Update(device)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	updated, err := mock.GetByID("test-device-3")
	if err != nil || updated == nil {
		t.Fatal("GetByID failed after update")
	}
	if updated.Name != "Updated Device 3" {
		t.Errorf("Expected name 'Updated Device 3', got '%s'", updated.Name)
	}
}

func TestCachedDeviceStoreDelete(t *testing.T) {
	mock := NewMockDeviceStore()
	cached := NewCachedDeviceStore(mock, nil)

	device := &models.Device{
		ID:        "test-device-4",
		Name:      "Test Device 4",
		Hostname:  "test4.local",
		OS:        "linux",
		Arch:      "amd64",
		DeviceKey: "test-key-4",
		Status:    "online",
	}

	err := mock.Create(device)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = cached.Delete("test-device-4")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = mock.GetByID("test-device-4")
	if err == nil {
		t.Error("Device still exists after delete")
	}
}

func TestCachedDeviceStoreUpdateLastSeen(t *testing.T) {
	mock := NewMockDeviceStore()
	cached := NewCachedDeviceStore(mock, nil)

	device := &models.Device{
		ID:        "test-device-5",
		Name:      "Test Device 5",
		Hostname:  "test5.local",
		OS:        "linux",
		Arch:      "amd64",
		DeviceKey: "test-key-5",
		Status:    "offline",
	}

	err := mock.Create(device)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = cached.UpdateLastSeen("test-device-5")
	if err != nil {
		t.Fatalf("UpdateLastSeen failed: %v", err)
	}

	updated, err := mock.GetByID("test-device-5")
	if err != nil || updated == nil {
		t.Fatal("GetByID failed")
	}
	if updated.Status != "online" {
		t.Errorf("Expected status 'online', got '%s'", updated.Status)
	}
}
