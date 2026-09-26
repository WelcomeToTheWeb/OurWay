package models

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// APIKey represents a user API key for programmatic access.
type APIKey struct {
	ID          string    `gorm:"type:uuid;primaryKey" json:"id"`
	UserID      string    `gorm:"type:uuid;not null;index" json:"-"`
	Name        string    `gorm:"not null" json:"name"`
	Key         string    `gorm:"uniqueIndex;not null" json:"key"`
	KeyHash     string    `gorm:"uniqueIndex;not null" json:"-"`
	Scopes      string    `gorm:"type:text;not null;default:'[]'" json:"scopes"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	RotatedAt   *time.Time `json:"rotated_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// BeforeCreate generates the key and hash before saving.
func (k *APIKey) BeforeCreate(tx *gorm.DB) (err error) {
	if k.ID == "" {
		k.ID = uuid.New().String()
	}
	if k.Key == "" {
		k.Key = GenerateAPIKey()
	}
	if k.KeyHash == "" {
		k.KeyHash = HashAPIKey(k.Key)
	}
	return nil
}

// GenerateAPIKey creates a prefixed API key: owk_<32 hex chars>.
func GenerateAPIKey() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "owk_" + uuid.New().String()
	}
	return "owk_" + hex.EncodeToString(b)
}

// HashAPIKey creates a SHA-256 hash of the API key for storage.
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// IsRevoked returns true if the key has been revoked.
func (k *APIKey) IsRevoked() bool {
	return k.RevokedAt != nil && !k.RevokedAt.IsZero()
}

// IsExpired returns true if the key has expired.
func (k *APIKey) IsExpired() bool {
	return k.ExpiresAt != nil && !k.ExpiresAt.IsZero() && time.Now().After(*k.ExpiresAt)
}

// IsActive returns true if the key is not revoked and not expired.
func (k *APIKey) IsActive() bool {
	return !k.IsRevoked() && !k.IsExpired()
}
