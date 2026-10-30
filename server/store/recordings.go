package store

import (
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// RecordingStore provides access to the 2.4-8 session recordings: the manifest
// rows. The frames themselves are a file on disk, and this store is the only
// place that states where it is.
type RecordingStore struct {
	db *gorm.DB
}

// Create inserts a recording row. The caller has already opened the file: a row
// naming a file nobody opened is a manifest of nothing.
func (s *RecordingStore) Create(rec *models.SessionRecording) error {
	return s.db.Create(rec).Error
}

// GetByID fetches one recording.
func (s *RecordingStore) GetByID(id string) (*models.SessionRecording, error) {
	var rec models.SessionRecording
	if err := s.db.First(&rec, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetBySession fetches the recording of one session. The unique index on
// `session_id` means there is at most one: a session that was recorded twice
// would be two recordings of the same screen, and the ledger could not say
// which is the frame a person saw.
func (s *RecordingStore) GetBySession(sessionID string) (*models.SessionRecording, error) {
	var rec models.SessionRecording
	if err := s.db.Where("session_id = ?", sessionID).First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// ListAll returns recordings newest first, for an admin (all tenants in scope).
func (s *RecordingStore) ListAll(limit int) ([]models.SessionRecording, error) {
	return s.list(limit, func(q *gorm.DB) *gorm.DB { return q })
}

// ListOwn returns one account's own recordings: what this person did, in their
// own sessions. A technician cannot read a colleague's recording, and the rule
// is stated by whose session it was.
func (s *RecordingStore) ListOwn(userID string, limit int) ([]models.SessionRecording, error) {
	return s.list(limit, func(q *gorm.DB) *gorm.DB { return q.Where("user_id = ?", userID) })
}

// ListScoped returns the recordings of the caller's **clients** (2.3.1), the same
// rule as every other tenant-facing ledger: a technician scoped to client A reads
// a client-B recording as a 404, and the statement carries the caller's tenant
// set, never their unit list (2.3-8).
func (s *RecordingStore) ListScoped(clientIDs []string, includeUnassigned bool, limit int) ([]models.SessionRecording, error) {
	query, keep := scopeByClient(s.db, s.db.Order("started_at DESC"), "device_id", clientIDs, includeUnassigned)
	if !keep {
		return []models.SessionRecording{}, nil
	}
	return s.listOn(query, limit)
}

func (s *RecordingStore) list(limit int, add func(*gorm.DB) *gorm.DB) ([]models.SessionRecording, error) {
	query := add(s.db.Order("started_at DESC"))
	return s.listOn(query, limit)
}

func (s *RecordingStore) listOn(query *gorm.DB, limit int) ([]models.SessionRecording, error) {
	if limit > 0 {
		query = query.Limit(limit)
	}
	var recs []models.SessionRecording
	if err := query.Find(&recs).Error; err != nil {
		return nil, err
	}
	return recs, nil
}

// SetPath states the file a recording row names. A row with no path is a
// manifest that points at nothing, so the writer's reopen path states it.
func (s *RecordingStore) SetPath(id, path string) error {
	return s.db.Model(&models.SessionRecording{}).Where("id = ?", id).Update("path", path).Error
}

// Flush records what the writer has appended since the last flush. The columns
// are named, so a live recording's manifest update cannot overwrite the fields
// that describe why it exists — `Reason` and `Policy` are written once.
func (s *RecordingStore) Flush(id string, frames, bytes int64, checksum string, interruptions int, dropped int64) error {
	return s.db.Model(&models.SessionRecording{}).Where("id = ?", id).Updates(map[string]any{
		"frame_count":    frames,
		"bytes":          bytes,
		"checksum":       checksum,
		"interruptions":  interruptions,
		"dropped_frames": dropped,
	}).Error
}

// End closes a recording: the session ended, the file is closed, and its stated
// size is final. A row with no `ended_at` is a recording being appended to, and
// playback reads it as one.
func (s *RecordingStore) End(id string, frames, bytes int64, checksum string, interruptions int, dropped int64) error {
	now := time.Now().UTC()
	return s.db.Model(&models.SessionRecording{}).Where("id = ?", id).Updates(map[string]any{
		"ended_at":       now,
		"frame_count":    frames,
		"bytes":          bytes,
		"checksum":       checksum,
		"interruptions":  interruptions,
		"dropped_frames": dropped,
	}).Error
}

// ListOpen returns recordings that were never closed — the rows a server restart
// leaves behind. The sweep closes them (the session is over, the writer is gone)
// rather than leaving a "recording" state that points at a file nobody writes to.
func (s *RecordingStore) ListOpen() ([]models.SessionRecording, error) {
	var recs []models.SessionRecording
	err := s.db.Where("ended_at IS NULL").Find(&recs).Error
	return recs, err
}

// ListPastRetention returns the rows whose retention window has closed, with
// their paths: the sweep needs the file, not just the row.
func (s *RecordingStore) ListPastRetention(cutoff time.Time) ([]models.SessionRecording, error) {
	var recs []models.SessionRecording
	err := s.db.Where("retention_until IS NOT NULL AND retention_until <= ?", cutoff).Find(&recs).Error
	return recs, err
}

// PruneOlderThan deletes the rows whose `retention_until` has passed and returns
// them, so the caller can remove the files they name. The rows go first: a file
// unlinked with its row still present is a recording the console offers and the
// reader cannot fetch, which is the failure the row exists to prevent.
func (s *RecordingStore) PruneOlderThan(cutoff time.Time) ([]models.SessionRecording, error) {
	recs, err := s.ListPastRetention(cutoff)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(recs))
	for i := range recs {
		ids = append(ids, recs[i].ID)
	}
	if err := s.db.Where("id IN (?)", ids).Delete(&models.SessionRecording{}).Error; err != nil {
		return nil, err
	}
	return recs, nil
}

// Count reports how many recordings exist. The sweep's counter feeds the console
// (2.5.0's metrics surface), and a recording count nobody can read is a disk
// usage nobody notices.
func (s *RecordingStore) Count() (int64, error) {
	var n int64
	if err := s.db.Model(&models.SessionRecording{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
