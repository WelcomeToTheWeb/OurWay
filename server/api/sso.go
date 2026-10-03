package api

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

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

	// pendingCodes holds one-time SSO login codes: after the provider
	// callback, the token pair is parked here for a short TTL and handed
	// to the browser via POST /api/auth/sso/exchange instead of the
	// redirect URL (C6: tokens must not sit in query strings/logs).
	mu      sync.Mutex
	pending map[string]*pendingSSOToken
}

// pendingSSOToken is a one-time token pair awaiting exchange.
type pendingSSOToken struct {
	access  string
	refresh string
	expires time.Time
}

// ssoCodeTTL is how long an un-exchanged SSO login code stays valid.
const ssoCodeTTL = 60 * time.Second

// NewSSOHandler creates a new SSO handler.
func NewSSOHandler(store *store.Store, jwtAuth *auth.JWTAuth, redirectURI string) *SSOHandler {
	return &SSOHandler{
		store:    store,
		jwtAuth:  jwtAuth,
		oauth:    sso.NewOAuthHandler(store, jwtAuth),
		redirect: redirectURI,
		pending:  make(map[string]*pendingSSOToken),
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
	if _, err := rand.Read(stateBytes); err != nil {
		log.Printf("sso: failed to generate state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	state := hex.EncodeToString(stateBytes)

	// Store state in session cookie. Secure=true: this is a CSRF state
	// cookie on a flow that must not be replayable over plaintext HTTP.
	c.SetCookie("sso_state", state, 300, "/", "", true, true)

	redirectURI := h.redirect + "/api/auth/sso/" + providerName + "/callback"
	authorizeURL := h.oauth.AuthorizeURL(provider, redirectURI, state)

	c.Redirect(http.StatusFound, authorizeURL)
}

// Callback handles the SSO provider's redirect back to us. It exchanges
// the provider code for a token pair, parks it under a short-lived one-time
// code, and redirects the browser to /login?sso_code=... — the tokens
// themselves never appear in a URL (C6). The login page exchanges the code
// via POST /api/auth/sso/exchange.
func (h *SSOHandler) Callback(c *gin.Context) {
	providerName := c.Param("provider")

	provider, err := h.store.SSOProviders.GetByName(providerName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	// Verify state, then consume the cookie so it cannot be replayed (M13).
	expectedState, err := c.Cookie("sso_state")
	if err != nil || expectedState != c.Query("state") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}
	c.SetCookie("sso_state", "", -1, "/", "", true, true)

	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing authorization code"})
		return
	}

	redirectURI := h.redirect + "/api/auth/sso/" + providerName + "/callback"
	token, refreshToken, err := h.oauth.HandleCallback(c.Request.Context(), provider, code, redirectURI)
	if err != nil {
		log.Printf("sso: callback failed for provider %s: %v", providerName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "SSO login failed"})
		return
	}

	loginCode, err := h.storePendingToken(token, refreshToken)
	if err != nil {
		log.Printf("sso: failed to store pending token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.Redirect(http.StatusFound, h.redirect+"/login?sso_code="+loginCode)
}

// Exchange redeems a one-time SSO login code for the token pair (C6).
// POST /api/auth/sso/exchange
func (h *SSOHandler) Exchange(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	tok, ok := h.takePendingToken(req.Code)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired SSO code"})
		return
	}

	// The refresh token keeps the SSO session alive; without it the session
	// dies when the access token expires.
	c.JSON(http.StatusOK, gin.H{
		"access_token":  tok.access,
		"refresh_token": tok.refresh,
	})
}

// storePendingToken parks a token pair under a fresh one-time code and
// returns the code. Expired entries are swept opportunistically.
func (h *SSOHandler) storePendingToken(access, refresh string) (string, error) {
	codeBytes := make([]byte, 16)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", err
	}
	code := hex.EncodeToString(codeBytes)

	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	for k, v := range h.pending {
		if now.After(v.expires) {
			delete(h.pending, k)
		}
	}
	h.pending[code] = &pendingSSOToken{
		access:  access,
		refresh: refresh,
		expires: now.Add(ssoCodeTTL),
	}
	return code, nil
}

// takePendingToken atomically removes and returns a pending token pair.
// A missing or expired code reports ok=false (one-time use, C6).
func (h *SSOHandler) takePendingToken(code string) (*pendingSSOToken, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tok, ok := h.pending[code]
	if !ok {
		return nil, false
	}
	delete(h.pending, code)
	if time.Now().After(tok.expires) {
		return nil, false
	}
	return tok, true
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

	// Only OAuth2 is implemented; SAML is rejected rather than accepted
	// and silently broken (L8).
	switch strings.ToLower(req.Type) {
	case "oauth2", "oauth":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported SSO type: only oauth2 is implemented (saml is not supported yet)"})
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
