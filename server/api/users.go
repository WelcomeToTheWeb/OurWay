package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"ourway/server/models"
	"ourway/server/store"
)

// UserHandler handles user management endpoints.
type UserHandler struct {
	store *store.Store
}

// NewUserHandler creates a user handler.
func NewUserHandler(store *store.Store) *UserHandler {
	return &UserHandler{store: store}
}

// ListUsers returns all users with their roles.
func (h *UserHandler) ListUsers(c *gin.Context) {
	users, err := h.store.Users.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}

	type userWithRoles struct {
		models.User
		Roles []string `json:"roles"`
	}

	result := make([]userWithRoles, len(users))
	for i, user := range users {
		roles, err := h.store.UserRoles.GetUserRoles(user.ID)
		roleNames := make([]string, 0)
		if err == nil {
			for _, r := range roles {
				roleNames = append(roleNames, r.Name)
			}
		}
		result[i] = userWithRoles{User: user, Roles: roleNames}
	}

	c.JSON(http.StatusOK, gin.H{"users": result})
}

// CreateUser creates a new user with specified roles.
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req struct {
		Username string   `json:"username" binding:"required"`
		Email    string   `json:"email" binding:"required"`
		Password string   `json:"password" binding:"required"`
		Roles    []string `json:"roles"` // role names
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

	// Assign roles
	if len(req.Roles) > 0 {
		roleIDs := make([]string, 0)
		for _, roleName := range req.Roles {
			role, err := h.store.Roles.GetRoleByName(roleName)
			if err != nil {
				continue // Skip unknown roles
			}
			roleIDs = append(roleIDs, role.ID)
		}
		if len(roleIDs) > 0 {
			h.store.UserRoles.SetUserRoles(user.ID, roleIDs)
		}
	}

	c.JSON(http.StatusCreated, gin.H{"user": user})
}

// UpdateUserRoles updates the roles assigned to a user.
func (h *UserHandler) UpdateUserRoles(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Roles []string `json:"roles" binding:"required"` // role names
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify user exists
	if _, err := h.store.Users.GetByID(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	roleIDs := make([]string, 0)
	for _, roleName := range req.Roles {
		role, err := h.store.Roles.GetRoleByName(roleName)
		if err != nil {
			continue
		}
		roleIDs = append(roleIDs, role.ID)
	}

	if err := h.store.UserRoles.SetUserRoles(id, roleIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update roles"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated", "roles": req.Roles})
}

// DeleteUser deletes a user.
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id := c.Param("id")

	// Remove role assignments first
	h.store.UserRoles.SetUserRoles(id, []string{})

	if err := h.store.Users.Delete(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
