package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ourway/server/models"
	"ourway/server/store"
)

// RoleHandler handles role management endpoints.
type RoleHandler struct {
	store *store.Store
}

// NewRoleHandler creates a role handler.
func NewRoleHandler(store *store.Store) *RoleHandler {
	return &RoleHandler{store: store}
}

// ListRoles returns all roles.
func (h *RoleHandler) ListRoles(c *gin.Context) {
	roles, err := h.store.Roles.ListRoles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list roles"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"roles": roles})
}

// GetRole retrieves a single role by ID.
func (h *RoleHandler) GetRole(c *gin.Context) {
	id := c.Param("id")
	role, err := h.store.Roles.GetRoleByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "role not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"role": role})
}

// CreateRole creates a new role.
func (h *RoleHandler) CreateRole(c *gin.Context) {
	var req struct {
		Name        string   `json:"name" binding:"required"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	role := &models.Role{
		Name:        req.Name,
		Description: req.Description,
		Permissions: req.Permissions,
	}
	if err := h.store.Roles.CreateRole(role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create role"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"role": role})
}

// UpdateRole updates an existing role.
func (h *RoleHandler) UpdateRole(c *gin.Context) {
	id := c.Param("id")
	role, err := h.store.Roles.GetRoleByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "role not found"})
		return
	}

	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != "" {
		role.Name = req.Name
	}
	if req.Description != "" {
		role.Description = req.Description
	}
	if req.Permissions != nil {
		role.Permissions = req.Permissions
	}

	if err := h.store.Roles.UpdateRole(role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update role"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"role": role})
}

// DeleteRole deletes a role.
func (h *RoleHandler) DeleteRole(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.Roles.DeleteRole(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "role not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
