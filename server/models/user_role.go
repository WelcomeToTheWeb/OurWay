package models

import "time"

// UserRole is the junction table linking users to roles.
type UserRole struct {
	ID        string    `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    string    `gorm:"type:uuid;index;not null" json:"user_id"`
	RoleID    string    `gorm:"type:uuid;index;not null" json:"role_id"`
	CreatedAt time.Time `json:"created_at"`
}
