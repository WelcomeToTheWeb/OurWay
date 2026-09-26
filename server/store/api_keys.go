package store

import (
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// APIKeyStore provides database operations for API keys.
type APIKeyStore struct {
	db *gorm.DB
}

// Create saves a new API key.
func (s *APIKeyStore) Create(k *models.APIKey) error {
	return s.db.Create(k).Error
}

// GetByID retrieves an API key by ID.
func (s *APIKeyStore) GetByID(id string) (*models.APIKey, error) {
	var k models.APIKey
	err := s.db.First(&k, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// GetByHash retrieves an API key by its hash (used for authentication).
func (s *APIKeyStore) GetByHash(hash string) (*models.APIKey, error) {
	var k models.APIKey
	err := s.db.First(&k, "key_hash = ?", hash).Error
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// ListByUser returns all API keys for a user.
func (s *APIKeyStore) ListByUser(userID string) ([]models.APIKey, error) {
	var keys []models.APIKey
	err := s.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&keys).Error
	return keys, err
}

// Update updates an existing API key.
func (s *APIKeyStore) Update(k *models.APIKey) error {
	return s.db.Save(k).Error
}

// Delete removes an API key.
func (s *APIKeyStore) Delete(id string) error {
	return s.db.Delete(&models.APIKey{}, "id = ?", id).Error
}

// Revoke marks an API key as revoked.
func (s *APIKeyStore) Revoke(id string) error {
	now := time.Now()
	return s.db.Model(&models.APIKey{}).Where("id = ?", id).Update("revoked_at", now).Error
}

// UpdateLastUsed updates the last used timestamp.
func (s *APIKeyStore) UpdateLastUsed(id string) error {
	now := time.Now()
	return s.db.Model(&models.APIKey{}).Where("id = ?", id).Update("last_used_at", now).Error
}
