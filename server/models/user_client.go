package models

import (
	"time"

	"github.com/google/uuid"
)

// UserClient is a client-scope grant: this user may see and act on this
// client's devices (2.3-0).
//
// Scope is *stored as rows*, not baked into the access token. Revoking a
// technician's client must take effect on their next request, not at their next
// login (2.3-1's acceptance criterion), and a token claim cannot be revoked
// while it is valid.
//
// An `admin` is scope-all **by role** and holds no rows: an admin with junction
// rows would be a scope that says "these clients" and the console would have to
// guess what the rows mean. Absence of rows for an admin is not "no scope",
// and the resolver never reads this table for admins.
type UserClient struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    string    `gorm:"type:uuid;uniqueIndex:idx_user_client;index;not null" json:"user_id"`
	ClientID  string    `gorm:"type:uuid;uniqueIndex:idx_user_client;index;not null" json:"client_id"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName is the plain table name; the unique index above is composite on
// (user_id, client_id) — one grant per pair. Granting the same client twice is
// not a scope, and a scope table that admits duplicates makes "how many clients
// can this technician see" a number nobody can trust.
func (UserClient) TableName() string { return "user_clients" }

// NewUserClientID mints the UUID a new grant row carries.
func NewUserClientID() string { return uuid.NewString() }
