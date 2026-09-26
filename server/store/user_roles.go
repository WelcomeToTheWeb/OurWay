package store

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/models"
)

// UserRoleStore provides operations for managing user-role assignments.
type UserRoleStore struct {
	db *gorm.DB
}

// AssignRole assigns a role to a user.
func (s *UserRoleStore) AssignRole(userID, roleID string) error {
	// Check if already assigned
	var count int64
	s.db.Model(&models.UserRole{}).
		Where("user_id = ? AND role_id = ?", userID, roleID).
		Count(&count)
	if count > 0 {
		return nil // Already assigned, no error
	}

	userRole := models.UserRole{
		ID:     uuid.New().String(),
		UserID: userID,
		RoleID: roleID,
	}
	return s.db.Create(&userRole).Error
}

// RemoveRole removes a role from a user.
func (s *UserRoleStore) RemoveRole(userID, roleID string) error {
	result := s.db.Where("user_id = ? AND role_id = ?", userID, roleID).
		Delete(&models.UserRole{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

// GetUserRoles returns all roles assigned to a user.
func (s *UserRoleStore) GetUserRoles(userID string) ([]models.Role, error) {
	var roles []models.Role
	err := s.db.Model(&models.Role{}).
		Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ?", userID).
		Find(&roles).Error
	return roles, err
}

// SetUserRoles replaces all roles for a user with the given set.
func (s *UserRoleStore) SetUserRoles(userID string, roleIDs []string) error {
	// Remove all existing assignments
	if err := s.db.Where("user_id = ?", userID).Delete(&models.UserRole{}).Error; err != nil {
		return err
	}

	// Assign new roles
	for _, roleID := range roleIDs {
		if err := s.AssignRole(userID, roleID); err != nil {
			return err
		}
	}
	return nil
}

// UserHasRole checks if a user has a specific role.
func (s *UserRoleStore) UserHasRole(userID, roleID string) (bool, error) {
	var count int64
	err := s.db.Model(&models.UserRole{}).
		Where("user_id = ? AND role_id = ?", userID, roleID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UserHasAnyRole checks if a user has any of the given roles.
func (s *UserRoleStore) UserHasAnyRole(userID string, roleIDs []string) (bool, error) {
	var count int64
	err := s.db.Model(&models.UserRole{}).
		Where("user_id = ? AND role_id IN ?", userID, roleIDs).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetUserRolesByNames returns users with any of the given role names.
func (s *UserRoleStore) GetUserRolesByNames(userID string, roleNames []string) ([]models.Role, error) {
	var roles []models.Role
	err := s.db.Model(&models.Role{}).
		Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ? AND roles.name IN ?", userID, roleNames).
		Find(&roles).Error
	if err != nil {
		return nil, err
	}
	return roles, nil
}
