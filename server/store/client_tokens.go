package store

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// ClientTokenStore is 2.3-4c: the enrollment credentials of a tenant.
//
// The store only ever sees hashes. The plaintext exists in the model as a
// create-time field and in the create endpoint's response, once.
type ClientTokenStore struct {
	db *gorm.DB
}

// Create stores a token. `models.ClientEnrollmentToken.BeforeCreate` generates
// the secret, so a caller that left `Token` empty gets the generated value back
// on the same struct.
func (s *ClientTokenStore) Create(t *models.ClientEnrollmentToken) error {
	return s.db.Create(t).Error
}

// ListByClient returns a tenant's tokens newest-first, revoked ones included:
// an operator asking "which of these did the field tech have" needs the revoked
// rows, and `revoked_at` is the column that says they are inert.
func (s *ClientTokenStore) ListByClient(clientID string) ([]models.ClientEnrollmentToken, error) {
	var out []models.ClientEnrollmentToken
	err := s.db.Where("client_id = ?", clientID).Order("created_at DESC").Find(&out).Error
	return out, err
}

// GetByHash is the registration lookup: the presented value is hashed by the
// caller and this walks `idx` on `token_hash`. Not found answers `(nil, nil)`,
// which is the same answer a bad value gets, so registration cannot be used to
// probe which tokens exist.
func (s *ClientTokenStore) GetByHash(hash string) (*models.ClientEnrollmentToken, error) {
	if hash == "" {
		return nil, nil
	}
	var t models.ClientEnrollmentToken
	err := s.db.Where("token_hash = ?", hash).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Revoke marks a token inert for a client. The id is scoped by client so a
// caller holding one tenant's token id cannot revoke another tenant's token
// through a route that was only checked against the caller's scope.
func (s *ClientTokenStore) Revoke(clientID, id string) error {
	now := time.Now().UTC()
	res := s.db.Model(&models.ClientEnrollmentToken{}).
		Where("id = ? AND client_id = ? AND revoked_at IS NULL", id, clientID).
		Update("revoked_at", now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// MarkUsed records the last enrollment through this token. An MSP rotates these
// after a build; the column is what makes "is this token still in use?" a read
// rather than a guess.
func (s *ClientTokenStore) MarkUsed(id string) error {
	now := time.Now().UTC()
	return s.db.Model(&models.ClientEnrollmentToken{}).
		Where("id = ?", id).
		Update("last_used_at", now).Error
}

// DeleteByClient drops a tenant's tokens with it. A credential that enrolls
// into a tenant that no longer exists is not a stale row, it is a live
// enrollment path into whatever tenant the id space gives next.
func (s *ClientTokenStore) DeleteByClient(clientID string) error {
	return s.db.Where("client_id = ?", clientID).Delete(&models.ClientEnrollmentToken{}).Error
}
