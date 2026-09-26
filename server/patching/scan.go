package patching

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// Scanner scans devices for software updates.
type Scanner struct {
	store *store.Store
	hub   *ws.Hub
	mu    sync.Mutex
}

// NewScanner creates a new patch scanner.
func NewScanner(store *store.Store, hub *ws.Hub) *Scanner {
	return &Scanner{
		store: store,
		hub:   hub,
	}
}

// ScanDevices sends scan commands to all online devices.
func (s *Scanner) ScanDevices(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	devices, err := s.store.Devices.ListAll()
	if err != nil {
		return fmt.Errorf("failed to list devices: %w", err)
	}

	var scanned int
	for _, device := range devices {
		if device.Status != "online" {
			continue
		}

		if err := s.hub.SendToDevice(device.DeviceKey, "scan_updates", map[string]interface{}{
			"device_id": device.ID,
		}); err != nil {
			log.Printf("patching: failed to send scan to device %s: %v", device.Name, err)
			continue
		}

		scanned++
		log.Printf("patching: sent scan to device %s", device.Name)
	}

	log.Printf("patching: scan sent to %d online devices", scanned)
	return nil
}

// ReportUpdate receives an update report from a device.
func (s *Scanner) ReportUpdate(deviceID string, update map[string]interface{}) error {
	softwareUpdate := &models.SoftwareUpdate{
		ID:       uuid.New().String(),
		DeviceID: deviceID,
		Source:   update["source"].(string),
		Title:    update["title"].(string),
		Version:  update["version"].(string),
		Status:   "detected",
	}

	if size, ok := update["size_bytes"]; ok {
		if sizeFloat, ok := size.(float64); ok {
			softwareUpdate.SizeBytes = int64(sizeFloat)
		}
	}

	return s.store.SoftwareUpdates.Create(softwareUpdate)
}

// ScanAll periodically scans all devices.
func (s *Scanner) ScanAll(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.ScanDevices(ctx); err != nil {
				log.Printf("patching: scan error: %v", err)
			}
		}
	}
}
