package models

import (
	"time"
)

// SSOProvider represents a configured SSO provider.
type SSOProvider struct {
	ID           string    `gorm:"type:uuid;primaryKey" json:"id"`
	Type         string    `gorm:"not null" json:"type"` // oauth2, oidc, saml
	Name         string    `gorm:"not null" json:"name"` // google, microsoft, apple, custom
	ClientID     string    `gorm:"not null" json:"client_id"`
	ClientSecret string    `gorm:"not null" json:"-"`
	AuthURL      string    `json:"auth_url"`
	TokenURL     string    `json:"token_url"`
	UserInfoURL  string    `json:"user_info_url"`
	Scope        string    `json:"scope"`
	Enabled      bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}
