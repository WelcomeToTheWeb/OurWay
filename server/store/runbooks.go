package store

import (
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// RunbookStore provides CRUD operations for runbooks.
type RunbookStore struct {
	db *gorm.DB
}

// NewRunbookStore creates a runbook store.
func NewRunbookStore(db *gorm.DB) *RunbookStore {
	return &RunbookStore{db: db}
}

// Create stores a new runbook.
func (s *RunbookStore) Create(r *models.Runbook) error {
	return s.db.Create(r).Error
}

// GetByID retrieves a runbook by ID.
func (s *RunbookStore) GetByID(id string) (*models.Runbook, error) {
	var r models.Runbook
	if err := s.db.First(&r, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

// ListAll returns all runbooks.
func (s *RunbookStore) ListAll() ([]models.Runbook, error) {
	var out []models.Runbook
	err := s.db.Order("created_at DESC").Find(&out).Error
	return out, err
}

// Update saves a runbook.
func (s *RunbookStore) Update(r *models.Runbook) error {
	return s.db.Save(r).Error
}

// Delete removes a runbook; deleting an unknown id is ErrRecordNotFound so
// the API can answer 404.
func (s *RunbookStore) Delete(id string) error {
	result := s.db.Delete(&models.Runbook{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CreateRun stores a run record.
func (s *RunbookStore) CreateRun(run *models.RunbookRun) error {
	return s.db.Create(run).Error
}

// UpdateRun saves a run record.
func (s *RunbookStore) UpdateRun(run *models.RunbookRun) error {
	return s.db.Save(run).Error
}

// GetRun retrieves a run by ID.
func (s *RunbookStore) GetRun(id string) (*models.RunbookRun, error) {
	var run models.RunbookRun
	if err := s.db.First(&run, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRunsByRunbook returns a runbook's most recent runs.
func (s *RunbookStore) ListRunsByRunbook(runbookID string, limit int) ([]models.RunbookRun, error) {
	var out []models.RunbookRun
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	err := s.db.Where("runbook_id = ?", runbookID).
		Order("created_at DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

// DeleteOldRuns prunes run records older than the given duration so
// runbook history does not grow without bound (see M11's lesson).
func (s *RunbookStore) DeleteOldRuns(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return s.db.Where("created_at < ?", cutoff).Delete(&models.RunbookRun{}).Error
}
