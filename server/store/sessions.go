package store

import (
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// SessionStore provides CRUD operations for remote sessions.
type SessionStore struct {
	db *gorm.DB
}

// Create inserts a new session.
func (s *SessionStore) Create(session *models.Session) error {
	return s.db.Create(session).Error
}

// GetByID fetches a session by its UUID.
func (s *SessionStore) GetByID(id string) (*models.Session, error) {
	var session models.Session
	if err := s.db.First(&session, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// GetLiveByTokenHash fetches the pending or active session whose remote
// token hashes to tokenHash.
func (s *SessionStore) GetLiveByTokenHash(tokenHash string) (*models.Session, error) {
	var session models.Session
	if err := s.db.First(&session, "remote_token_hash = ? AND status IN (?)", tokenHash, []string{"pending", "active"}).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// GetLiveByViewerTokenHash fetches the pending or active session whose
// viewer token hashes to tokenHash.
func (s *SessionStore) GetLiveByViewerTokenHash(tokenHash string) (*models.Session, error) {
	var session models.Session
	if err := s.db.First(&session, "viewer_token_hash = ? AND status IN (?)", tokenHash, []string{"pending", "active"}).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// Update persists changes to a session record.
func (s *SessionStore) Update(session *models.Session) error {
	return s.db.Save(session).Error
}

// Delete removes a session by ID.
func (s *SessionStore) Delete(id string) error {
	return s.db.Delete(&models.Session{}, "id = ?", id).Error
}

// EndSession marks a session as ended with the current timestamp.
func (s *SessionStore) EndSession(id string) error {
	now := time.Now()
	return s.db.Model(&models.Session{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":   "ended",
			"ended_at": now,
		}).Error
}

// ListActive returns all active sessions for a device.
func (s *SessionStore) ListActive(deviceID string) ([]models.Session, error) {
	var sessions []models.Session
	if err := s.db.Find(&sessions, "device_id = ? AND status IN (?)", deviceID, []string{"pending", "active"}).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

// ListActiveAll returns all pending and active sessions across devices.
func (s *SessionStore) ListActiveAll() ([]models.Session, error) {
	var sessions []models.Session
	if err := s.db.Find(&sessions, "status IN (?)", []string{"pending", "active"}).Order("created_at DESC").Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

// ListStalePending returns "pending" sessions created before cutoff
// (the agent never delivered a frame for them — H6 reaper input).
func (s *SessionStore) ListStalePending(cutoff time.Time) ([]models.Session, error) {
	var sessions []models.Session
	if err := s.db.Find(&sessions, "status = ? AND created_at < ?", "pending", cutoff).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

// ListActiveByUser returns the user's own pending and active sessions (M6).
func (s *SessionStore) ListActiveByUser(userID string) ([]models.Session, error) {
	var sessions []models.Session
	if err := s.db.Find(&sessions, "user_id = ? AND status IN (?)", userID, []string{"pending", "active"}).Order("created_at DESC").Error; err != nil {
		return nil, err
	}
	return sessions, nil
}
