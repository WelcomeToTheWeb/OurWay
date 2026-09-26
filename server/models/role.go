package models

import "time"

// Role represents a user role with a set of permissions.
type Role struct {
	ID          string    `gorm:"type:uuid;primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	Description string    `json:"description"`
	Permissions []string  `gorm:"serializer:json" json:"permissions"`
	IsBuiltin   bool      `gorm:"not null;default:false" json:"is_builtin"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// HasPermission checks if the role has the specified permission.
func (r *Role) HasPermission(perm string) bool {
	for _, p := range r.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// BuiltinRole defines a built-in role with its default permissions.
type BuiltinRole struct {
	Name        string
	Description string
	Permissions []string
}

// DefaultBuiltinRoles returns the default set of built-in roles.
func DefaultBuiltinRoles() []BuiltinRole {
	return []BuiltinRole{
		{
			Name:        "admin",
			Description: "Full system access with all permissions",
			Permissions: []string{
				"devices:read", "devices:write", "devices:delete",
				"users:read", "users:write", "users:delete",
				"roles:read", "roles:write", "roles:delete",
				"alerts:read", "alerts:write",
				"settings:read", "settings:write",
			},
		},
		{
			Name:        "manager",
			Description: "Manage devices, users, and alerts; cannot modify roles or settings",
			Permissions: []string{
				"devices:read", "devices:write", "devices:delete",
				"users:read", "users:write",
				"roles:read",
				"alerts:read", "alerts:write",
				"settings:read",
			},
		},
		{
			Name:        "technician",
			Description: "View and manage devices; read-only for users and roles",
			Permissions: []string{
				"devices:read", "devices:write",
				"users:read",
				"roles:read",
				"alerts:read", "alerts:write",
			},
		},
		{
			Name:        "viewer",
			Description: "Read-only access to devices, users, and alerts",
			Permissions: []string{
				"devices:read",
				"users:read",
				"roles:read",
				"alerts:read",
			},
		},
	}
}
