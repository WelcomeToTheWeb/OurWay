package models

import "time"

// User represents an RMM user account.
type User struct {
	ID             string     `gorm:"type:uuid;primaryKey" json:"id"`
	Username       string     `gorm:"uniqueIndex;not null" json:"username"`
	Email          string     `gorm:"not null" json:"email"`
	PasswordHash   string     `gorm:"default:''" json:"-"`
	Provider       string     `gorm:"not null;default:local;index" json:"provider"`
	ProviderID     string     `gorm:"not null;default:'';index" json:"provider_id"`
	SSOAttributes  string     `gorm:"type:text;not null;default:'{}'" json:"sso_attributes"`
	Roles          []Role     `gorm:"many2many:user_roles;joinForeignKey:UserID;joinReferences:RoleID" json:"roles,omitempty"`
	UserRoles      []UserRole `gorm:"foreignKey:UserID" json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
}
