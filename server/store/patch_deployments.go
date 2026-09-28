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
	timeout := now.Add(30 * time.Minute)
	return s.db.Model(&models.PatchDeployment{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        "running",
			"devices_total": total,
			"started_at":    now,
			"timeout_at":    timeout,
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
			"status":       "completed",
			"completed_at": now,
		}).Error
}

// SetCounters replaces the success/failed counters with authoritative
// values (derived from the per-device result rows), making result reporting
// idempotent.
func (s *PatchDeploymentStore) SetCounters(id string, success, failed int) error {
	return s.db.Model(&models.PatchDeployment{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"devices_success": success,
			"devices_failed":  failed,
		}).Error
}

// FinalizeTimedOut marks running deployments whose timeout has passed as
// failed. This covers devices that were offline (or whose agents never
// reported) — without it such deployments would stay "running" forever.
// Returns the number of deployments finalized.
func (s *PatchDeploymentStore) FinalizeTimedOut() (int64, error) {
	now := time.Now()
	res := s.db.Model(&models.PatchDeployment{}).
		Where("status = ? AND timeout_at IS NOT NULL AND timeout_at <= ?", "running", now).
		Updates(map[string]interface{}{
			"status":       "failed",
			"completed_at": now,
			"message":      "timed out: one or more devices did not report a result",
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// FinishIfComplete transitions a running deployment to "completed" when all
// targeted devices have reported success, or to "failed" when at least one
// device reported failure. Returns the deployment's status after the check;
// the status is unchanged while devices are still outstanding.
func (s *PatchDeploymentStore) FinishIfComplete(id string) (string, error) {
	var deployment models.PatchDeployment
	if err := s.db.First(&deployment, "id = ?", id).Error; err != nil {
		return "", err
	}

	if deployment.Status != "running" {
		return deployment.Status, nil
	}

	if deployment.DevicesTotal > 0 && deployment.DevicesSuccess+deployment.DevicesFailed >= deployment.DevicesTotal {
		status := "completed"
		if deployment.DevicesFailed > 0 {
			status = "failed"
		}
		now := time.Now()
		if err := s.db.Model(&models.PatchDeployment{}).
			Where("id = ? AND status = ?", id, "running").
			Updates(map[string]interface{}{
				"status":       status,
				"completed_at": now,
			}).Error; err != nil {
			return "", err
		}
		return status, nil
	}

	return deployment.Status, nil
}
