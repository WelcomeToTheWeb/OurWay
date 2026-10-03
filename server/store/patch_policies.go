package store

import (
	"gorm.io/gorm"

	"ourway/server/models"
)

// PatchPolicyStore provides CRUD operations for patch policies.
type PatchPolicyStore struct {
	db *gorm.DB
}

// Create inserts a new patch policy. The explicit Select forces the
// zero-value-able fields into the INSERT: GORM omits zero-value fields
// that carry a default tag on create, so a policy created with
// approval_required=false (or auto_reboot=false) would silently get the
// column default (true) instead and never auto-approve.
func (s *PatchPolicyStore) Create(policy *models.PatchPolicy) error {
	return s.db.Select(
		"Name", "Scope", "ScopeValue", "Schedule",
		"AutoReboot", "ApprovalRequired", "MaxDevicesPerBatch",
	).Create(policy).Error
}

// GetByID fetches a patch policy by ID.
func (s *PatchPolicyStore) GetByID(id string) (*models.PatchPolicy, error) {
	var policy models.PatchPolicy
	if err := s.db.First(&policy, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

// ListAll returns all patch policies.
func (s *PatchPolicyStore) ListAll() ([]models.PatchPolicy, error) {
	var policies []models.PatchPolicy
	if err := s.db.Find(&policies).Error; err != nil {
		return nil, err
	}
	return policies, nil
}

// Update persists changes to a patch policy.
func (s *PatchPolicyStore) Update(policy *models.PatchPolicy) error {
	return s.db.Save(policy).Error
}

// Delete removes a patch policy by ID.
func (s *PatchPolicyStore) Delete(id string) error {
	return s.db.Delete(&models.PatchPolicy{}, "id = ?", id).Error
}
