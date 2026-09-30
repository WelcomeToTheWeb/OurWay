package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/sso"
	"ourway/server/store"
)

// SSOHandler handles SSO authentication and provider management.
type SSOHandler struct {
	store    *store.Store
	jwtAuth  *auth.JWTAuth
	oauth    *sso.OAuthHandler
	redirect string
}

// NewSSOHandler creates a new SSO handler.
func NewSSOHandler(store *store.Store, jwtAuth *auth.JWTAuth, redirectURI string) *SSOHandler {
	return &SSOHandler{
		store:    store,
		jwtAuth:  jwtAuth,
		oauth:    sso.NewOAuthHandler(store, jwtAuth),
		redirect: redirectURI,
	}
}

// ListProviders returns the configured SSO providers for the login page.
func (h *SSOHandler) ListProviders(c *gin.Context) {
	providers, err := h.store.SSOProviders.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list providers"})
		return
	}

	type providerInfo struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}

	var enabled []providerInfo
	for _, p := range providers {
		if p.Enabled {
			enabled = append(enabled, providerInfo{Type: p.Type, Name: p.Name})
		}
	}

	c.JSON(http.StatusOK, gin.H{"providers": enabled})
}

// Authorize redirects the user to the SSO provider's authorization page.
func (h *SSOHandler) Authorize(c *gin.Context) {
	providerName := c.Param("provider")

	provider, err := h.store.SSOProviders.GetByName(providerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// Generate state for CSRF protection
	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)

	// Store state in session cookie. Secure=true: this is a CSRF state
	// cookie on a flow that must not be replayable over plaintext HTTP.
	c.SetCookie("sso_state", state, 300, "/", "", true, true)

	redirectURI := h.redirect + "/api/auth/sso/" + providerName + "/callback"
	authorizeURL := h.oauth.AuthorizeURL(provider, redirectURI, state)

	c.Redirect(http.StatusFound, authorizeURL)
}

// Callback handles the SSO provider's redirect back to us.
func (h *SSOHandler) Callback(c *gin.Context) {
	providerName := c.Param("provider")

	provider, err := h.store.SSOProviders.GetByName(providerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// Verify state
	expectedState, err := c.Cookie("sso_state")
	if err != nil || expectedState != c.Query("state") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing authorization code"})
		return
	}

	redirectURI := h.redirect + "/api/auth/sso/" + providerName + "/callback"
	token, refreshToken, err := h.oauth.HandleCallback(c.Request.Context(), provider, code, redirectURI)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("SSO callback failed: %s", err.Error())})
		return
	}

	// Redirect to frontend with access + refresh tokens (the refresh token
	// keeps the SSO session alive; without it the session dies after the
	// access token expires).
	c.Redirect(http.StatusFound, h.redirect+"/login?token="+token+"&refresh="+refreshToken)
}

// applyProviderDefaults fills in well-known endpoint presets for named
// providers (google, microsoft, apple) without overwriting values that
// are already set.
func applyProviderDefaults(provider *models.SSOProvider, name string) {
	var preset struct {
		AuthURL     string
		TokenURL    string
		UserInfoURL string
		Scope       string
	}
	switch name {
	case "google":
		preset = struct {
			AuthURL     string
			TokenURL    string
			UserInfoURL string
			Scope       string
		}{
			AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:    "https://oauth2.googleapis.com/token",
			UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
			Scope:       "openid profile email",
		}
	case "microsoft":
		preset = struct {
			AuthURL     string
			TokenURL    string
			UserInfoURL string
			Scope       string
		}{
			AuthURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			UserInfoURL: "https://graph.microsoft.com/oidc/userinfo",
			Scope:       "openid profile email offline_access",
		}
	case "apple":
		preset = struct {
			AuthURL     string
			TokenURL    string
			UserInfoURL string
			Scope       string
		}{
			AuthURL:     "https://appleid.apple.com/auth/authorize",
			TokenURL:    "https://appleid.apple.com/auth/token",
			UserInfoURL: "https://appleid.apple.com/auth/userinfo",
			Scope:       "name email",
		}
	}

	if preset.AuthURL == "" {
		return
	}
	if provider.AuthURL == "" {
		provider.AuthURL = preset.AuthURL
	}
	if provider.TokenURL == "" {
		provider.TokenURL = preset.TokenURL
	}
	if provider.UserInfoURL == "" {
		provider.UserInfoURL = preset.UserInfoURL
	}
	if provider.Scope == "" {
		provider.Scope = preset.Scope
	}
}

// CreateProvider creates or updates an SSO provider (admin).
func (h *SSOHandler) CreateProvider(c *gin.Context) {
	var req struct {
		Type         string `json:"type" binding:"required"`
		Name         string `json:"name" binding:"required"`
		ClientID     string `json:"client_id" binding:"required"`
		ClientSecret string `json:"client_secret"`
		AuthURL      string `json:"auth_url"`
		TokenURL     string `json:"token_url"`
		UserInfoURL  string `json:"user_info_url"`
		Scope        string `json:"scope"`
		Enabled      *bool  `json:"enabled"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if provider with this name exists
	existing, err := h.store.SSOProviders.GetByName(req.Name)
	var provider *models.SSOProvider

	if err == nil && existing != nil {
		// Update existing
		provider = existing
		provider.Type = req.Type
		provider.ClientID = req.ClientID
		if req.ClientSecret != "" {
			provider.ClientSecret = req.ClientSecret
		}
		// Apply provider-specific defaults (see create branch), then let
		// explicit request values win. Values that are not provided must
		// keep the preset defaults — do not clobber them with empty strings.
		applyProviderDefaults(provider, req.Name)
		if req.AuthURL != "" {
			provider.AuthURL = req.AuthURL
		}
		if req.TokenURL != "" {
			provider.TokenURL = req.TokenURL
		}
		if req.UserInfoURL != "" {
			provider.UserInfoURL = req.UserInfoURL
		}
		if req.Scope != "" {
			provider.Scope = req.Scope
		}
		if req.Enabled != nil {
			provider.Enabled = *req.Enabled
		}

		if err := h.store.SSOProviders.Update(provider); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update provider"})
			return
		}
	} else {
		// Create new with provider-specific defaults
		provider = &models.SSOProvider{
			ID:           uuid.New().String(),
			Type:         req.Type,
			Name:         req.Name,
			ClientID:     req.ClientID,
			ClientSecret: req.ClientSecret,
			Scope:        req.Scope,
			Enabled:      true,
		}

		// Apply provider-specific defaults, then let explicit request
		// values win (only when provided — empty values must not clobber
		// the presets).
		applyProviderDefaults(provider, req.Name)
		if req.AuthURL != "" {
			provider.AuthURL = req.AuthURL
		}
		if req.TokenURL != "" {
			provider.TokenURL = req.TokenURL
		}
		if req.UserInfoURL != "" {
			provider.UserInfoURL = req.UserInfoURL
		}
		if req.Scope != "" {
			provider.Scope = req.Scope
		}
		if req.Enabled != nil {
			provider.Enabled = *req.Enabled
		}

		if err := h.store.SSOProviders.Create(provider); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create provider"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"provider": provider})
}

// ListProvidersAdmin returns all SSO providers with details (admin).
func (h *SSOHandler) ListProvidersAdmin(c *gin.Context) {
	providers, err := h.store.SSOProviders.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list providers"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"providers": providers})
}

// DeleteProvider removes an SSO provider (admin).
func (h *SSOHandler) DeleteProvider(c *gin.Context) {
	providerID := c.Param("id")

	if err := h.store.SSOProviders.Delete(providerID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "provider deleted"})
}
