package store

import (
	"gorm.io/gorm"

	"ourway/server/models"
)

// UserStore provides persistence for user accounts.
type UserStore struct {
	db *gorm.DB
}

// Create inserts a new user.
func (s *UserStore) Create(u *models.User) error {
	return s.db.Create(u).Error
}

// Update saves changes to an existing user.
func (s *UserStore) Update(u *models.User) error {
	return s.db.Save(u).Error
}

// FindByEmail finds a user by email.
func (s *UserStore) FindByEmail(email string) (*models.User, error) {
	var u models.User
	if err := s.db.First(&u, "email = ?", email).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByProvider finds a user by SSO provider and provider ID.
func (s *UserStore) FindByProvider(provider, providerID string) (*models.User, error) {
	var u models.User
	if err := s.db.First(&u, "provider = ? AND provider_id = ?", provider, providerID).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// GetByUsername fetches a user by their username.
func (s *UserStore) GetByUsername(username string) (*models.User, error) {
	var u models.User
	if err := s.db.First(&u, "username = ?", username).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// GetByID fetches a user by their ID.
func (s *UserStore) GetByID(id string) (*models.User, error) {
	var u models.User
	if err := s.db.Preload("Roles").First(&u, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// ListAll returns every user.
func (s *UserStore) ListAll() ([]models.User, error) {
	var users []models.User
	if err := s.db.Preload("Roles").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// Count returns the total number of users.
func (s *UserStore) Count() (int64, error) {
	var count int64
	if err := s.db.Model(&models.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Delete removes a user by ID.
func (s *UserStore) Delete(id string) error {
	result := s.db.Delete(&models.User{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
