package store

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ourway/server/models"
)

// RoleStore provides CRUD operations for roles.
type RoleStore struct {
	db *gorm.DB
}

// CreateRole creates a new role.
func (s *RoleStore) CreateRole(role *models.Role) error {
	role.ID = uuid.New().String()
	return s.db.Create(role).Error
}

// GetRoleByID retrieves a role by its ID.
func (s *RoleStore) GetRoleByID(id string) (*models.Role, error) {
	var role models.Role
	if err := s.db.First(&role, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("role not found")
		}
		return nil, err
	}
	return &role, nil
}

// GetRoleByName retrieves a role by its name.
func (s *RoleStore) GetRoleByName(name string) (*models.Role, error) {
	var role models.Role
	if err := s.db.First(&role, "name = ?", name).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("role not found")
		}
		return nil, err
	}
	return &role, nil
}

// ListRoles returns all roles.
func (s *RoleStore) ListRoles() ([]models.Role, error) {
	var roles []models.Role
	if err := s.db.Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// UpdateRole updates an existing role.
func (s *RoleStore) UpdateRole(role *models.Role) error {
	return s.db.Model(&models.Role{}).Where("id = ?", role.ID).Updates(role).Error
}

// DeleteRole deletes a role.
func (s *RoleStore) DeleteRole(id string) error {
	result := s.db.Delete(&models.Role{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("role not found")
	}
	return nil
}

// SeedBuiltinRoles creates built-in roles if they don't already exist.
func (s *RoleStore) SeedBuiltinRoles() error {
	for _, builtin := range models.DefaultBuiltinRoles() {
		existing, err := s.GetRoleByName(builtin.Name)
		if err == nil && existing != nil {
			// Role already exists, skip
			continue
		}

		role := &models.Role{
			ID:          uuid.New().String(),
			Name:        builtin.Name,
			Description: builtin.Description,
			Permissions: builtin.Permissions,
			IsBuiltin:   true,
		}
		if err := s.db.Create(role).Error; err != nil {
			return fmt.Errorf("failed to seed role %s: %w", builtin.Name, err)
		}
	}
	return nil
}
