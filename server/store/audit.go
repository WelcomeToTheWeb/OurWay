package store

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"ourway/server/models"
)

// AuditStore is the audit log's writer and reader.
//
// The writer is a store method, called by the handlers (2.3-6's rule: **not** a
// gin middleware). Middleware cannot know the target, the client, or whether the
// action succeeded — and all three are the record. A middleware that guessed
// them would produce a log nobody can act on, which is the failure mode the
// phase-1 "complete" item shipped with.
type AuditStore struct {
	db *gorm.DB
}

// NewAuditStore builds the ledger's store over an existing database. The
// retention manager takes one directly, so the sweep that prunes the ledger is
// testable without constructing a whole installation.
func NewAuditStore(db *gorm.DB) *AuditStore { return &AuditStore{db: db} }

// AuditRecord is one act, as a handler knows it.
type AuditRecord struct {
	Action     string
	TargetType string
	TargetID   string
	ClientID   string
	Outcome    string
	Details    map[string]any
}

// Record writes one act.
//
// Actor fields are filled from the request context by `api.audit`, which is the
// only caller: one definition of "who is this request" (2.3-1's
// `CallerUserID`), so a row cannot log an actor the request never carried.
func (s *AuditStore) Record(row *models.AuditLog) error {
	if row.ID == "" {
		row.ID = models.NewAuditID()
	}
	return s.db.Create(row).Error
}

// EncodeDetails serialises a details map, keeping the column's shape uniform.
func EncodeDetails(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// AuditFilter is the ledger's query.
type AuditFilter struct {
	ActorUserID string
	Action      string
	ClientIDs   []string
	Since       *time.Time
	Until       *time.Time
	Limit       int
	Offset      int
}

// List reads audit rows newest-first.
//
// `ClientIDs` is the scope filter, and an **empty non-nil slice answers no
// rows**: a manager with no grants reads no audit history. `nil` means
// unrestricted, which only admin reaches (the API layer decides, the same way
// the device lists do).
func (s *AuditStore) List(f AuditFilter) ([]models.AuditLog, error) {
	query := s.db.Order("created_at DESC")
	if f.ActorUserID != "" {
		query = query.Where("actor_user_id = ?", f.ActorUserID)
	}
	if f.Action != "" {
		query = query.Where("action = ?", f.Action)
	}
	if f.ClientIDs != nil {
		if len(f.ClientIDs) == 0 {
			return []models.AuditLog{}, nil
		}
		query = query.Where("client_id IN ?", f.ClientIDs)
	}
	if f.Since != nil {
		query = query.Where("created_at >= ?", *f.Since)
	}
	if f.Until != nil {
		query = query.Where("created_at <= ?", *f.Until)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 500
	}
	// The ledger is the one table in the product that grows without a bound,
	// so paging is not a nicety: page 1 must be cheap and bounded, and an
	// unbounded read is what turns "show me the log" into a full-table scan on
	// the busiest index in the installation.
	var rows []models.AuditLog
	query = query.Limit(limit)
	if f.Offset > 0 {
		query = query.Offset(f.Offset)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// PruneOlderThan deletes rows past the retention window and reports how many
// went. M11's lesson is the shape of this method: a delete with no caller is
// not a retention policy, so `retention.go` calls it on the same schedule that
// prunes metric history, and the counter is observable.
func (s *AuditStore) PruneOlderThan(cutoff time.Time) (int64, error) {
	res := s.db.Where("created_at < ?", cutoff).Delete(&models.AuditLog{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// Count reports the ledger's size. The retention manager reads it every tick
// so "the log is being pruned" is observable (M11: a cleanup nobody can see is
// a cleanup that quietly stopped running).
func (s *AuditStore) Count() int64 {
	var n int64
	if err := s.db.Model(&models.AuditLog{}).Count(&n).Error; err != nil {
		return -1
	}
	return n
}

// CountOldestThan supports the retention tests and the metrics counter without
// a second query shape.
func (s *AuditStore) CountOldestThan(cutoff time.Time) (int64, error) {
	var n int64
	if err := s.db.Model(&models.AuditLog{}).Where("created_at < ?", cutoff).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
