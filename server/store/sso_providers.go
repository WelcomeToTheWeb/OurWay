package store

import (
	"gorm.io/gorm"

	"ourway/server/models"
)

// SSOProviderStore provides CRUD operations for SSO providers.
type SSOProviderStore struct {
	db *gorm.DB
}

// NewSSOProviderStore creates a new SSO provider store.
func NewSSOProviderStore(db *gorm.DB) *SSOProviderStore {
	return &SSOProviderStore{db: db}
}

// Create stores a new SSO provider.
func (s *SSOProviderStore) Create(provider *models.SSOProvider) error {
	return s.db.Create(provider).Error
}

// GetByID retrieves an SSO provider by ID.
func (s *SSOProviderStore) GetByID(id string) (*models.SSOProvider, error) {
	var provider models.SSOProvider
	err := s.db.First(&provider, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &provider, nil
}

// GetByName retrieves an SSO provider by name.
func (s *SSOProviderStore) GetByName(name string) (*models.SSOProvider, error) {
	var provider models.SSOProvider
	err := s.db.First(&provider, "name = ? AND enabled = ?", name, true).Error
	if err != nil {
		return nil, err
	}
	return &provider, nil
}

// ListAll returns all SSO providers.
func (s *SSOProviderStore) ListAll() ([]models.SSOProvider, error) {
	var providers []models.SSOProvider
	err := s.db.Order("created_at DESC").Find(&providers).Error
	return providers, err
}

// Update updates an existing SSO provider.
func (s *SSOProviderStore) Update(provider *models.SSOProvider) error {
	return s.db.Save(provider).Error
}

// Delete removes an SSO provider.
func (s *SSOProviderStore) Delete(id string) error {
	return s.db.Delete(&models.SSOProvider{}, "id = ?", id).Error
}
