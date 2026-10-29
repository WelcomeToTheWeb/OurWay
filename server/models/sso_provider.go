package models

import (
	"time"
)

// Link policies (2.4-3), per provider.
//
// `pending` is the default and the safe state: a provider that asserts the email of
// an existing local account does **not** get to log in as that account. The
// assertion creates a pending identity an admin must confirm.
//
// `link` is the pre-2.4 behaviour (match by email, stamp the provider onto the
// account). It exists so an operator who has read what it means can choose it; it
// is not what a new provider gets.
const (
	SSOLinkPolicyPending = "pending"
	SSOLinkPolicyLink    = "link"
)

// SSOProvider represents a configured SSO provider.
//
// From 2.4-1 this row is an **OIDC provider**, configured by issuer, and the
// endpoints are the ones the provider publishes. The three fields that predate
// that (`AuthURL`/`TokenURL`/`UserInfoURL`) stay, because they are what the flow
// uses — 2.4-1 changes who fills them in (discovery, not a hardcoded switch) and
// adds the pieces verification needs (`Issuer`, `JwksURI`).
type SSOProvider struct {
	ID           string    `gorm:"type:uuid;primaryKey" json:"id"`
	Type         string    `gorm:"not null" json:"type"` // oauth2 or oidc (saml is not implemented)
	Name         string    `gorm:"not null" json:"name"` // a slug: google, keycloak-corp, …
	ClientID     string    `gorm:"not null" json:"client_id"`
	ClientSecret string    `gorm:"not null" json:"-"`
	AuthURL      string    `json:"auth_url"`
	TokenURL     string    `json:"token_url"`
	UserInfoURL  string    `json:"user_info_url"`
	Scope        string    `json:"scope"`
	Enabled      bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`

	// --- 2.4-1 · issuer, discovery, and the identity contract's knobs.
	//
	// `Issuer` is what an operator types. Everything below it is what the
	// provider said about itself, fetched at save time: a provider row that was
	// saved from a guess is a login that fails at 2 a.m. with a 500.
	Issuer       string `json:"issuer"`
	DiscoveryURL string `json:"discovery_url"`
	// `JwksURI` is the key set the `id_token` is verified against (2.4-2).
	JwksURI      string     `json:"jwks_uri"`
	RedirectURI  string     `json:"redirect_uri"`
	DiscoveryDoc string     `json:"-"` // the document as published, for the preview
	DiscoveredAt *time.Time `json:"discovered_at"`
	// PKCESupported is what discovery said about `code_challenge_methods_supported`,
	// recorded at save time: the authorize redirect has to decide whether to send a
	// challenge, and it must not pay for a round trip to the IdP to find out (2.4-2).
	PKCESupported bool `gorm:"not null;default:false" json:"pkce_supported"`

	// Claim mapping: which claim carries which fact. IdPs do not agree on this,
	// and the product must not guess. Empty means the OIDC standard names.
	ClaimsEmail    string `json:"claims_email"`
	ClaimsUsername string `json:"claims_username"`
	ClaimsName     string `json:"claims_name"`
	ClaimsGroups   string `json:"claims_groups"`

	// The 2.4-3 security contract, per provider.
	// `AllowedDomains` is a comma-separated list; an asserted email outside it is
	// refused. `LinkPolicy` is `pending` (default: a new provider/email pair never
	// touches an existing account) or `link` (the pre-2.4 behaviour, kept for
	// operators who choose it knowingly).
	AllowedDomains       string `json:"allowed_domains"`
	RequireVerifiedEmail bool   `gorm:"not null;default:false" json:"require_verified_email"`
	LinkPolicy           string `gorm:"not null;default:'pending'" json:"link_policy"`
	// `RoleMapping` is JSON of claim value -> role. Empty is not "everyone is
	// admin": it means no role mapping, and the `viewer` floor stands.
	RoleMapping string `json:"role_mapping"`

	// The login page lists providers in this order (2.4-1: several enabled at once).
	SortOrder int `gorm:"not null;default:0" json:"sort_order"`
}
