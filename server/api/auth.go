package api

import (
	"net/http"
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

	// Assign default viewer role to new users
	if err := h.assignDefaultRole(user.ID); err != nil {
		// Log but don't fail registration
		c.JSON(http.StatusCreated, gin.H{"user": user})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user": user})
}

// assignDefaultRole assigns the viewer role to a new user.
func (h *AuthHandler) assignDefaultRole(userID string) error {
	viewerRole, err := h.store.Roles.GetRoleByName("viewer")
	if err != nil {
		return err
	}
	return h.store.UserRoles.AssignRole(userID, viewerRole.ID)
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
