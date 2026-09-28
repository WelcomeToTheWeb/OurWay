package store

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/models"
)

// DeploymentResultStore stores per-device deployment outcomes.
type DeploymentResultStore struct {
	db *gorm.DB
}

// NewDeploymentResultStore creates a deployment result store.
func NewDeploymentResultStore(db *gorm.DB) *DeploymentResultStore {
	return &DeploymentResultStore{db: db}
}

// UpsertResult records (or updates) the outcome for one device in a
// deployment. It returns true when the stored result changed (new row, or a
// different result was reported) so callers can adjust the deployment
// counters exactly once per transition.
func (s *DeploymentResultStore) UpsertResult(deploymentID, deviceID, result string) (bool, error) {
	var existing models.DeploymentResult
	err := s.db.Where("deployment_id = ? AND device_id = ?", deploymentID, deviceID).
		First(&existing).Error

	if err == nil {
		if existing.Result == result {
			// Duplicate report of the same outcome: idempotent no-op.
			return false, nil
		}
		existing.Result = result
		existing.UpdatedAt = time.Now()
		if err := s.db.Save(&existing).Error; err != nil {
			return false, err
		}
		return true, nil
	}

	if err != gorm.ErrRecordNotFound {
		return false, err
	}

	row := &models.DeploymentResult{
		ID:           uuid.New().String(),
		DeploymentID: deploymentID,
		DeviceID:     deviceID,
		Result:       result,
	}
	if err := s.db.Create(row).Error; err != nil {
		// Lost a race with a concurrent insert: treat as no-op, the other
		// writer already recorded it.
		return false, nil
	}
	return true, nil
}

// Counts returns the number of success/failed results recorded for a
// deployment.
func (s *DeploymentResultStore) Counts(deploymentID string) (success, failed int, err error) {
	var sCount, fCount int64
	if err := s.db.Model(&models.DeploymentResult{}).
		Where("deployment_id = ? AND result = ?", deploymentID, "success").
		Count(&sCount).Error; err != nil {
		return 0, 0, err
	}
	if err := s.db.Model(&models.DeploymentResult{}).
		Where("deployment_id = ? AND result = ?", deploymentID, "failed").
		Count(&fCount).Error; err != nil {
		return 0, 0, err
	}
	return int(sCount), int(fCount), nil
}
