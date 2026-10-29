package models

import "time"

// User represents an RMM user account.
type User struct {
	ID            string     `gorm:"type:uuid;primaryKey" json:"id"`
	Username      string     `gorm:"uniqueIndex;not null" json:"username"`
	Email         string     `gorm:"not null" json:"email"`
	PasswordHash  string     `gorm:"default:''" json:"-"`
	Provider      string     `gorm:"not null;default:local;index" json:"provider"`
	ProviderID    string     `gorm:"not null;default:'';index" json:"provider_id"`
	SSOAttributes string     `gorm:"type:text;not null;default:'{}'" json:"sso_attributes"`
	Roles         []Role     `gorm:"many2many:user_roles;joinForeignKey:UserID;joinReferences:RoleID" json:"roles,omitempty"`
	UserRoles     []UserRole `gorm:"foreignKey:UserID" json:"-"`
	CreatedAt     time.Time  `json:"created_at"`

	// --- 2.4-3 · the identity-linking contract.
	//
	// `Status` is `active` or `pending`. A pending account is an identity an identity
	// provider asserted whose email belongs to an account that already exists: OurWay
	// does not link those by itself, whatever the IdP asserts. The row is created with
	// **no roles**, it cannot log in, and it names the account the assertion points at
	// until an admin approves it as its own account, merges it into that one, or
	// refuses it.
	Status string `gorm:"not null;default:'active';index" json:"status"`
	// `PendingProvider` is the door the assertion came from; `PendingLinkUserID` is
	// the existing account the asserted email belongs to. Both are recorded for the
	// admin who has to decide, and for the audit row that records the decision.
	PendingProvider   string `gorm:"not null;default:''" json:"pending_provider"`
	PendingLinkUserID string `gorm:"not null;default:''" json:"pending_link_user_id"`
}

// Account statuses (2.4-3).
const (
	UserStatusActive  = "active"
	UserStatusPending = "pending"
)

// IsPending reports the state the auth paths must refuse: a pending identity has no
// roles, no password, and no access.
func (u *User) IsPending() bool {
	return u != nil && u.Status == UserStatusPending
}
