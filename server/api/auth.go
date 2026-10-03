package api

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"ourway/server/auth"
	"ourway/server/models"
	"ourway/server/store"
)

// AuthHandler handles user authentication endpoints.
type AuthHandler struct {
	store   *store.Store
	jwtAuth *auth.JWTAuth
}

// NewAuthHandler creates an auth handler.
func NewAuthHandler(store *store.Store, jwtAuth *auth.JWTAuth) *AuthHandler {
	return &AuthHandler{store: store, jwtAuth: jwtAuth}
}

// Status returns whether any users exist (for first-run registration detection).
func (h *AuthHandler) Status(c *gin.Context) {
	count, err := h.store.Users.Count()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check user count"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"has_users": count > 0})
}

// Register handles user registration.
//
// Registration is only open while the install has zero users (first
// run). After that it returns 403 (C1) - further accounts are created
// by an admin via the user API.
func (h *AuthHandler) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if count, err := h.store.Users.Count(); err == nil && count > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "registration is closed; ask an administrator for an account"})
		return
	}

	// Duplicate e-mail is a conflict, not an internal error (L5).
	if existing, err := h.store.Users.FindByEmail(req.Email); err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "a user with this e-mail already exists"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	user := &models.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}

	if err := h.store.Users.Create(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	// A user without roles cannot log in usefully (role loading breaks
	// their session); fail registration loudly instead (L4).
	// The very first account bootstraps the install: it gets the admin
	// role instead of viewer, so a fresh deployment is manageable.
	roleName := "viewer"
	if count, err := h.store.Users.Count(); err == nil && count <= 1 {
		roleName = "admin"
	}
	if err := h.assignRole(user.ID, roleName); err != nil {
		log.Printf("auth: failed to assign %s role to user %s: %v", roleName, user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user created, but role assignment failed; re-register"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user": user})
}

// assignRole assigns the named builtin role to a user.
func (h *AuthHandler) assignRole(userID, roleName string) error {
	role, err := h.store.Roles.GetRoleByName(roleName)
	if err != nil {
		return err
	}
	return h.store.UserRoles.AssignRole(userID, role.ID)
}

// Login handles user login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.store.Users.GetByUsername(req.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	// Load user roles
	roles, err := h.store.UserRoles.GetUserRoles(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load roles"})
		return
	}

	roleNames := make([]string, len(roles))
	for i, r := range roles {
		roleNames[i] = r.Name
	}

	accessToken, err := h.jwtAuth.GenerateToken(user.ID, user.Username, roleNames)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	refreshToken, err := h.jwtAuth.GenerateRefreshToken(user.ID, user.Username, roleNames)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate refresh token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"user":          user,
		"roles":         roleNames,
	})
}

// Refresh exchanges a valid refresh token for a new access token.
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	claims, err := h.jwtAuth.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}

	user, err := h.store.Users.GetByID(claims.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	// Load current roles so role changes are reflected in the new token
	roles, err := h.store.UserRoles.GetUserRoles(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load roles"})
		return
	}

	roleNames := make([]string, len(roles))
	for i, r := range roles {
		roleNames[i] = r.Name
	}

	accessToken, err := h.jwtAuth.GenerateToken(user.ID, user.Username, roleNames)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": accessToken,
	})
}

// GetProfile returns the current user's profile and roles.
func (h *AuthHandler) GetProfile(c *gin.Context) {
	userID, _ := c.Get("user_id")
	user, err := h.store.Users.GetByID(userID.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	roles, err := h.store.UserRoles.GetUserRoles(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load roles"})
		return
	}

	roleNames := make([]string, len(roles))
	for i, r := range roles {
		roleNames[i] = r.Name
	}

	c.JSON(http.StatusOK, gin.H{
		"user":  user,
		"roles": roleNames,
	})
}

// UpdateProfile updates the caller's username and/or email.
// PUT /api/auth/profile
func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)
	user, err := h.store.Users.GetByID(userIDStr)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	if req.Username != "" {
		user.Username = req.Username
	}
	if req.Email != "" {
		if existing, err := h.store.Users.FindByEmail(req.Email); err == nil && existing != nil && existing.ID != user.ID {
			c.JSON(http.StatusConflict, gin.H{"error": "a user with this e-mail already exists"})
			return
		}
		user.Email = req.Email
	}

	if err := h.store.Users.Update(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": user})
}

// UpdatePassword changes the caller's password after verifying the current one.
// PUT /api/auth/password
func (h *AuthHandler) UpdatePassword(c *gin.Context) {
	var req struct {
		CurrentPassword string `json:"current_password" binding:"required"`
		NewPassword     string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.NewPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new password must be at least 8 characters"})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)
	user, err := h.store.Users.GetByID(userIDStr)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	if user.Provider != "local" || user.PasswordHash == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "password change is not available for SSO accounts"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}
	user.PasswordHash = string(hash)

	if err := h.store.Users.Update(user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
		return
	}

	// Invalidate every access token issued before this password change
	// (H3); refresh tokens stay valid until their own expiry.
	h.jwtAuth.BumpUserGen(user.ID)

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// DeleteAccount deletes the caller's own account (Danger Zone).
// The user's role assignments and API keys are removed as well.
// DELETE /api/auth/me
func (h *AuthHandler) DeleteAccount(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	user, err := h.store.Users.GetByID(userIDStr)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// Revoke the user's API keys and clear role assignments.
	if keys, err := h.store.APIKeys.ListByUser(user.ID); err == nil {
		for _, k := range keys {
			if err := h.store.APIKeys.Delete(k.ID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete API keys"})
				return
			}
		}
	}
	h.store.UserRoles.SetUserRoles(user.ID, []string{})

	if err := h.store.Users.Delete(user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete account"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// Logout revokes the caller's current access token (H3): its JTI is
// added to the in-memory denylist until the token would have expired
// anyway. Refresh tokens remain valid - the client should also drop
// them locally.
// POST /api/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	claims, err := h.validateBearer(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	h.jwtAuth.Deny(claims.ID, claims.ExpiresAt)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// validateBearer parses and validates the Authorization: Bearer token
// (shared by Logout; the middleware does the same for the API group).
func (h *AuthHandler) validateBearer(c *gin.Context) (*auth.Claims, error) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if token == "" || token == c.GetHeader("Authorization") {
		return nil, errors.New("missing token")
	}
	return h.jwtAuth.ValidateToken(token)
}
