package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"ourway/server/models"
	"ourway/server/store"
	"ourway/server/ws"
)

// Service handles file transfer operations.
type Service struct {
	store *store.Store
	hub   *ws.Hub
	dir   string // directory to store uploaded files
}

// NewService creates a new file transfer service.
func NewService(store *store.Store, hub *ws.Hub, dir string) *Service {
	if dir == "" {
		dir = "/tmp/ourway-files"
	}
	os.MkdirAll(dir, 0755)
	return &Service{
		store: store,
		hub:   hub,
		dir:   dir,
	}
}

// UploadFile stores an uploaded file on the server under a new transfer ID.
func (s *Service) UploadFile(filename string, data []byte) (string, error) {
	transferID := uuid.New().String()
	return s.StoreFile(transferID, data)
}

// StoreFile stores file bytes under the given transfer ID.
func (s *Service) StoreFile(transferID string, data []byte) (string, error) {
	dest := filepath.Join(s.dir, transferID)
	if err := os.WriteFile(dest, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return dest, nil
}

// CreateTransfer stores a file transfer record.
func (s *Service) CreateTransfer(transfer *models.FileTransfer) error {
	return s.store.FileTransfers.Create(transfer)
}

// GetFile returns the path to an uploaded file.
func (s *Service) GetFile(transferID string) (string, error) {
	path := filepath.Join(s.dir, transferID)
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

// PushFile initiates a file push to a device.
func (s *Service) PushFile(ctx context.Context, sourceID string, deviceID string, destination string, filename string, size int64) (*models.FileTransfer, error) {
	// The hub keys device clients by "device:"+device_key, so look up the
	// device to send by its key, not its UUID.
	device, err := s.store.Devices.GetByID(deviceID)
	if err != nil {
		return nil, fmt.Errorf("device not found: %w", err)
	}

	// One transfer row per device with a distinct ID (the source row is
	// shared). Copy the stored bytes under the new ID so the agent can
	// download the file by this transfer ID.
	transferID := uuid.New().String()
	if srcPath, err := s.GetFile(sourceID); err == nil {
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read stored file: %w", err)
		}
		if _, err := s.StoreFile(transferID, data); err != nil {
			return nil, err
		}
	}

	transfer := &models.FileTransfer{
		ID:          transferID,
		DeviceID:    deviceID,
		Filename:    filename,
		Destination: destination,
		SizeBytes:   size,
		Status:      "pending",
		Direction:   "push",
	}

	if err := s.store.FileTransfers.Create(transfer); err != nil {
		return nil, fmt.Errorf("failed to create transfer record: %w", err)
	}

	// Send command to agent via WebSocket
	s.hub.SendToDevice(device.DeviceKey, "file_push", map[string]interface{}{
		"transfer_id": transferID,
		"filename":    filename,
		"destination": destination,
		"size_bytes":  size,
	})

	return transfer, nil
}

// PullFile initiates a file pull from a device.
func (s *Service) PullFile(ctx context.Context, deviceID string, sourcePath string) (*models.FileTransfer, error) {
	transferID := uuid.New().String()

	device, err := s.store.Devices.GetByID(deviceID)
	if err != nil {
		return nil, fmt.Errorf("device not found: %w", err)
	}

	transfer := &models.FileTransfer{
		ID:         transferID,
		DeviceID:   deviceID,
		Filename:   filepath.Base(sourcePath),
		SourcePath: sourcePath,
		Status:     "pending",
		Direction:  "pull",
	}

	if err := s.store.FileTransfers.Create(transfer); err != nil {
		return nil, fmt.Errorf("failed to create transfer record: %w", err)
	}

	// Send command to agent via WebSocket (hub keys clients by device key)
	s.hub.SendToDevice(device.DeviceKey, "file_pull", map[string]interface{}{
		"transfer_id": transferID,
		"source_path": sourcePath,
		"filename":    filepath.Base(sourcePath),
	})

	return transfer, nil
}

// UpdateStatus updates the status of a transfer.
func (s *Service) UpdateStatus(transferID string, status string, progress int, errMsg string) error {
	return s.store.FileTransfers.UpdateStatus(transferID, status, progress, errMsg)
}

// ListTransfers returns recent file transfers.
func (s *Service) ListTransfers() ([]models.FileTransfer, error) {
	return s.store.FileTransfers.ListAll()
}

// ListDeviceTransfers returns transfers for a specific device.
func (s *Service) ListDeviceTransfers(deviceID string) ([]models.FileTransfer, error) {
	return s.store.FileTransfers.ListByDevice(deviceID)
}

// GetTransfer returns a specific transfer.
func (s *Service) GetTransfer(transferID string) (*models.FileTransfer, error) {
	return s.store.FileTransfers.GetByID(transferID)
}

// CleanupOldFiles removes uploaded files older than maxAge.
func (s *Service) CleanupOldFiles(maxAge time.Duration) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(s.dir, entry.Name()))
		}
	}
}
