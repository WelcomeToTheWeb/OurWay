package store

import (
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// PatchDeploymentStore provides CRUD operations for patch deployments.
type PatchDeploymentStore struct {
	db *gorm.DB
}

// Create inserts a new patch deployment.
func (s *PatchDeploymentStore) Create(deployment *models.PatchDeployment) error {
	return s.db.Create(deployment).Error
}

// GetByID fetches a patch deployment by ID.
func (s *PatchDeploymentStore) GetByID(id string) (*models.PatchDeployment, error) {
	var deployment models.PatchDeployment
	if err := s.db.First(&deployment, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &deployment, nil
}

// ListAll returns all patch deployments.
func (s *PatchDeploymentStore) ListAll() ([]models.PatchDeployment, error) {
	var deployments []models.PatchDeployment
	if err := s.db.Order("created_at desc").Find(&deployments).Error; err != nil {
		return nil, err
	}
	return deployments, nil
}

// Update persists changes to a patch deployment.
func (s *PatchDeploymentStore) Update(deployment *models.PatchDeployment) error {
	return s.db.Save(deployment).Error
}

// MarkStarted marks a deployment as started.
func (s *PatchDeploymentStore) MarkStarted(id string, total int) error {
	now := time.Now()
	return s.db.Model(&models.PatchDeployment{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        "running",
			"devices_total": total,
			"started_at":    now,
		}).Error
}

// IncrementSuccess increments the success count.
func (s *PatchDeploymentStore) IncrementSuccess(id string) error {
	return s.db.Model(&models.PatchDeployment{}).
		Where("id = ?", id).
		UpdateColumn("devices_success", gorm.Expr("devices_success + 1")).Error
}

// IncrementFailed increments the failed count.
func (s *PatchDeploymentStore) IncrementFailed(id string) error {
	return s.db.Model(&models.PatchDeployment{}).
		Where("id = ?", id).
		UpdateColumn("devices_failed", gorm.Expr("devices_failed + 1")).Error
}

// MarkCompleted marks a deployment as completed.
func (s *PatchDeploymentStore) MarkCompleted(id string) error {
	now := time.Now()
	return s.db.Model(&models.PatchDeployment{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":      "completed",
			"completed_at": now,
		}).Error
}
