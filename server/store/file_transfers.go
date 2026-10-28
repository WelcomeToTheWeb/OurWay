package store

import (
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// FileTransferStore provides CRUD operations for file transfers.
type FileTransferStore struct {
	db *gorm.DB
}

// NewFileTransferStore creates a new file transfer store.
func NewFileTransferStore(db *gorm.DB) *FileTransferStore {
	return &FileTransferStore{db: db}
}

// Create stores a new file transfer record.
func (s *FileTransferStore) Create(transfer *models.FileTransfer) error {
	return s.db.Create(transfer).Error
}

// GetByID retrieves a file transfer by ID.
func (s *FileTransferStore) GetByID(id string) (*models.FileTransfer, error) {
	var transfer models.FileTransfer
	err := s.db.First(&transfer, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &transfer, nil
}

// ListByDevice returns all file transfers for a device.
func (s *FileTransferStore) ListByDevice(deviceID string) ([]models.FileTransfer, error) {
	var transfers []models.FileTransfer
	err := s.db.Where("device_id = ?", deviceID).Order("created_at DESC").Find(&transfers).Error
	return transfers, err
}

// ListScoped returns the transfers of the caller's **clients**, plus transfers
// that have no unit yet (an upload staged before it is pushed — it carries no
// unit's data, so scoping it would be scoping the wrong thing).
//
// The client set goes into the statement as a subquery over the fleet, not as a
// list of unit ids (2.3-8): the budget is the caller's tenant count, never
// their unit count.
func (s *FileTransferStore) ListScoped(clientIDs []string, includeUnassigned bool) ([]models.FileTransfer, error) {
	query := s.db.Order("created_at DESC").Where("device_id = ''")
	if len(clientIDs) == 0 && !includeUnassigned {
		// No grants: the unit-less transfers are all this caller can be shown.
		var staged []models.FileTransfer
		if err := query.Find(&staged).Error; err != nil {
			return nil, err
		}
		return staged, nil
	}
	query = query.Where("device_id IN (?)", visibleUnitIDs(s.db, clientIDs, includeUnassigned))
	var transfers []models.FileTransfer
	if err := query.Find(&transfers).Error; err != nil {
		return nil, err
	}
	return transfers, nil
}

// ListAll returns all file transfers, most recent first.
func (s *FileTransferStore) ListAll() ([]models.FileTransfer, error) {
	var transfers []models.FileTransfer
	err := s.db.Order("created_at DESC").Limit(100).Find(&transfers).Error
	return transfers, err
}

// UpdateStatus updates the status of a file transfer.
func (s *FileTransferStore) UpdateStatus(id string, status string, progress int, errMsg string) error {
	updates := map[string]interface{}{
		"status":        status,
		"progress":      progress,
		"error_message": errMsg,
		"updated_at":    time.Now(),
	}
	if status == "completed" || status == "failed" {
		now := time.Now()
		updates["completed_at"] = now
	}
	return s.db.Model(&models.FileTransfer{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateSize updates the file size of a transfer.
func (s *FileTransferStore) UpdateSize(id string, size int64) error {
	return s.db.Model(&models.FileTransfer{}).Where("id = ?", id).Update("size_bytes", size).Error
}
