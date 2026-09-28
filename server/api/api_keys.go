package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
)

// APIKeyHandler handles API key management and authentication.
type APIKeyHandler struct {
	store   *store.Store
	jwtAuth *auth.JWTAuth
}

// CreateAPIKeyHandler creates a new API key handler.
func CreateAPIKeyHandler(store *store.Store, jwtAuth *auth.JWTAuth) *APIKeyHandler {
	return &APIKeyHandler{store: store, jwtAuth: jwtAuth}
}

// CreateKeyRequest is the request body for creating an API key.
type CreateKeyRequest struct {
	Name     string   `json:"name" binding:"required"`
	Scopes   []string `json:"scopes"`
	Expires  string   `json:"expires"` // "never", "1h", "24h", "7d", "30d", "90d"
}

// RotateKeyRequest is the request body for rotating an API key.
type RotateKeyRequest struct {
	Expires string `json:"expires"`
}

// CreateKey creates a new API key for the user.
func (h *APIKeyHandler) CreateKey(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req CreateKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Scopes == nil || len(req.Scopes) == 0 {
		req.Scopes = []string{"read", "write"}
	}

	scopesJSON, _ := json.Marshal(req.Scopes)

	key := &models.APIKey{
		UserID:   userID.(string),
		Name:     req.Name,
		Scopes:   string(scopesJSON),
	}

	if req.Expires != "" && req.Expires != "never" {
		if exp, err := parseExpiration(req.Expires); err == nil {
			key.ExpiresAt = &exp
		}
	}

	if err := h.store.APIKeys.Create(key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create API key"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"key": key})
}

// ListKeys lists all API keys for the user.
func (h *APIKeyHandler) ListKeys(c *gin.Context) {
	userID, _ := c.Get("user_id")

	keys, err := h.store.APIKeys.ListByUser(userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list API keys"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"keys": keys})
}

// GetKey retrieves a specific API key.
func (h *APIKeyHandler) GetKey(c *gin.Context) {
	userID, _ := c.Get("user_id")
	keyID := c.Param("id")

	key, err := h.store.APIKeys.GetByID(keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	if key.UserID != userID.(string) {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"key": key})
}

// UpdateKey updates an existing API key.
func (h *APIKeyHandler) UpdateKey(c *gin.Context) {
	userID, _ := c.Get("user_id")
	keyID := c.Param("id")

	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	key, err := h.store.APIKeys.GetByID(keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	if key.UserID != userID.(string) {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	if req.Name != "" {
		key.Name = req.Name
	}
	if req.Scopes != nil {
		scopesJSON, _ := json.Marshal(req.Scopes)
		key.Scopes = string(scopesJSON)
	}

	if err := h.store.APIKeys.Update(key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"key": key})
}

// DeleteKey deletes an API key.
func (h *APIKeyHandler) DeleteKey(c *gin.Context) {
	userID, _ := c.Get("user_id")
	keyID := c.Param("id")

	key, err := h.store.APIKeys.GetByID(keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	if key.UserID != userID.(string) {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	if err := h.store.APIKeys.Delete(keyID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key deleted"})
}

// RevokeKey revokes an API key.
func (h *APIKeyHandler) RevokeKey(c *gin.Context) {
	userID, _ := c.Get("user_id")
	keyID := c.Param("id")

	key, err := h.store.APIKeys.GetByID(keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	if key.UserID != userID.(string) {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	if err := h.store.APIKeys.Revoke(keyID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key revoked"})
}

// RotateKey rotates an API key (generates a new key).
func (h *APIKeyHandler) RotateKey(c *gin.Context) {
	userID, _ := c.Get("user_id")
	keyID := c.Param("id")

	key, err := h.store.APIKeys.GetByID(keyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	if key.UserID != userID.(string) {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return
	}

	// Generate new key
	key.Key = models.GenerateAPIKey()
	key.KeyHash = models.HashAPIKey(key.Key)
	now := time.Now()
	key.RotatedAt = &now

	if err := h.store.APIKeys.Update(key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rotate API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"key": key, "message": "API key rotated"})
}

// parseExpiration parses a human-readable expiration string.
func parseExpiration(s string) (time.Time, error) {
	switch s {
	case "1h":
		return time.Now().Add(1 * time.Hour), nil
	case "24h", "1d":
		return time.Now().Add(24 * time.Hour), nil
	case "7d":
		return time.Now().Add(7 * 24 * time.Hour), nil
	case "30d":
		return time.Now().Add(30 * 24 * time.Hour), nil
	case "90d":
		return time.Now().Add(90 * 24 * time.Hour), nil
	default:
		return time.Time{}, fmt.Errorf("invalid expiration: %s", s)
	}
}

// APIKeyMiddleware authenticates requests using an API key in the X-API-Key header.
// It also accepts the Bearer token format for convenience.
func APIKeyMiddleware(store *store.Store, jwtAuth *auth.JWTAuth) gin.HandlerFunc {
	return func(c *gin.Context) {
		// First try API key from X-API-Key header
		apiKey := c.GetHeader("X-API-Key")

		// Fall back to Bearer token if it looks like an API key (owk_ prefix)
		if apiKey == "" {
			authHeader := c.GetHeader("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") && strings.HasPrefix(strings.TrimPrefix(authHeader, "Bearer "), "owk_") {
				apiKey = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if apiKey != "" {
			keyHash := models.HashAPIKey(apiKey)
			key, err := store.APIKeys.GetByHash(keyHash)
			if err == nil && key.IsActive() {
				// Enforce the key's stored scopes: read-only keys may not
				// perform mutating requests (and vice versa). An empty
				// scope list is treated as no access.
				var scopes []string
				json.Unmarshal([]byte(key.Scopes), &scopes)
				if !scopeAllows(scopes, c.Request.Method) {
					c.JSON(http.StatusForbidden, gin.H{"error": "api key scope does not permit this request"})
					c.Abort()
					return
				}

				// API key is valid - set user context
				c.Set("user_id", key.UserID)
				c.Set("api_key_id", key.ID)
				c.Set("api_key_scopes", key.Scopes)

				// Update last used timestamp (best effort)
				go store.APIKeys.UpdateLastUsed(key.ID)

				c.Next()
				return
			}
			// If API key provided but invalid, return 401
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired API key"})
			c.Abort()
			return
		}

		// No API key, fall through to JWT auth
		claims, err := jwtAuth.ValidateToken(getBearerToken(c))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("roles", claims.Roles)
		c.Next()
	}
}

func getBearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	return authHeader
}

// scopeAllows reports whether a key with the given scopes may perform a
// request with the given HTTP method: read-only methods need "read", all
// other methods need "write".
func scopeAllows(scopes []string, method string) bool {
	needed := "write"
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		needed = "read"
	}
	for _, s := range scopes {
		if s == needed {
			return true
		}
	}
	return false
}
