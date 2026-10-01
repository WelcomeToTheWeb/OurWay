package sso

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
)

// OAuthHandler handles OAuth 2.0 authentication flows.
type OAuthHandler struct {
	store   *store.Store
	jwtAuth *auth.JWTAuth
	client  *http.Client
}

// NewOAuthHandler creates a new OAuth handler.
func NewOAuthHandler(store *store.Store, jwtAuth *auth.JWTAuth) *OAuthHandler {
	return &OAuthHandler{
		store:   store,
		jwtAuth: jwtAuth,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// AuthorizeURL generates the OAuth 2.0 authorization URL.
func (h *OAuthHandler) AuthorizeURL(provider *models.SSOProvider, redirectURI string, state string) string {
	scope := provider.Scope
	if scope == "" {
		scope = "openid profile email"
	}

	params := url.Values{}
	params.Set("client_id", provider.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", scope)
	params.Set("state", state)

	authURL := provider.AuthURL + "?" + params.Encode()
	return authURL
}

// ExchangeCode exchanges an authorization code for an access token.
func (h *OAuthHandler) ExchangeCode(provider *models.SSOProvider, code, redirectURI string) (*OAuthTokens, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", provider.ClientID)
	form.Set("client_secret", provider.ClientSecret)

	resp, err := h.client.PostForm(provider.TokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token request returned %d", resp.StatusCode)
	}

	var tokenResp OAuthTokens
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}

// GetUserInfo retrieves user info from the OAuth provider.
func (h *OAuthHandler) GetUserInfo(provider *models.SSOProvider, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequest("GET", provider.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user info request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("user info request returned %d", resp.StatusCode)
	}

	var info UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	// Normalize fields based on provider type
	switch provider.Type {
	case "google":
		if info.Name == "" {
			info.Name = info.Email
		}
	case "microsoft":
		if info.Email == "" {
			info.Email = info.UserPrincipalName
		}
		if info.Name == "" {
			info.Name = info.Email
		}
	}

	return &info, nil
}

// OAuthTokens represents OAuth 2.0 tokens.
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
}

// UserInfo represents user info from an OAuth provider.
type UserInfo struct {
	Subject           string `json:"sub"`
	Name              string `json:"name"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Picture           string `json:"picture"`
	UserPrincipalName string `json:"upn"`
}

// HandleCallback handles the OAuth callback and returns a JWT access token
// and refresh token so the browser can keep the session alive past the
// access token's lifetime.
func (h *OAuthHandler) HandleCallback(ctx context.Context, provider *models.SSOProvider, code, redirectURI string) (string, string, error) {
	// Exchange code for token
	tokens, err := h.ExchangeCode(provider, code, redirectURI)
	if err != nil {
		return "", "", err
	}

	// Get user info
	info, err := h.GetUserInfo(provider, tokens.AccessToken)
	if err != nil {
		return "", "", err
	}

	// Find or create user (JIT provisioning)
	user, err := h.findOrCreateUser(ctx, provider, info, tokens.IDToken)
	if err != nil {
		return "", "", err
	}

	// Generate JWT
	roles := []string{"viewer"}
	if len(user.Roles) > 0 {
		roles = make([]string, 0, len(user.Roles))
		for _, role := range user.Roles {
			roles = append(roles, role.Name)
		}
	}

	token, err := h.jwtAuth.GenerateToken(user.ID, user.Username, roles)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate JWT: %w", err)
	}

	refreshToken, err := h.jwtAuth.GenerateRefreshToken(user.ID, user.Username, roles)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return token, refreshToken, nil
}

// findOrCreateUser implements JIT user provisioning.
func (h *OAuthHandler) findOrCreateUser(ctx context.Context, provider *models.SSOProvider, info *UserInfo, idToken string) (*models.User, error) {
	// Try to find by provider ID first
	if info.Subject != "" {
		user, err := h.store.Users.FindByProvider(provider.Name, info.Subject)
		if err == nil && user != nil {
			return user, nil
		}
	}

	// Try to find by email
	if info.Email != "" {
		user, err := h.store.Users.FindByEmail(info.Email)
		if err == nil && user != nil {
			// Update with provider info
			user.Provider = provider.Name
			user.ProviderID = info.Subject
			h.store.Users.Update(user)
			return user, nil
		}
	}

	// Create new user (JIT provisioning)
	email := info.Email
	if email == "" {
		email = info.Subject + "@" + provider.Name + ".sso"
	}

	username := email
	if idx := strings.Index(email, "@"); idx > 0 {
		username = email[:idx]
	}

	user := &models.User{
		ID:            uuid.New().String(),
		Username:      username,
		Email:         email,
		PasswordHash:  "",
		Provider:      provider.Name,
		ProviderID:    info.Subject,
		SSOAttributes: "",
	}

	// Store ID token in attributes
	if idToken != "" {
		user.SSOAttributes = fmt.Sprintf(`{"id_token":"%s"}`, idToken)
	}

	if err := h.store.Users.Create(user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}
