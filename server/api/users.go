package api

import (
	"encoding/json"
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

	// Creating an account creates a person the ledger will later name, so the
	// record fixes the username and the roles it started with.
	defer auditForAnswer(c, h.store, models.AuditUserCreate, AuditTarget{Type: "user", ID: user.ID}, map[string]any{
		"username": user.Username,
		"roles":    req.Roles,
	})
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

	// A role change is the row that matters most in an MSP's log: it is how a
	// viewer becomes someone who can reboot a fleet.
	defer auditForAnswer(c, h.store, models.AuditUserRoleChange, AuditTarget{Type: "user", ID: id}, map[string]any{
		"roles": req.Roles,
	})
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

	target := AuditTarget{Type: "user", ID: id}
	if existing, err := h.store.Users.GetByID(id); err == nil && existing != nil {
		// Read the account before it goes: after the delete there is no row to
		// ask which username was removed.
		target = AuditTarget{Type: "user", ID: existing.ID}
	}
	defer auditForAnswer(c, h.store, models.AuditUserDelete, target, nil)
	if err := h.store.Users.Delete(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// ListPendingIdentities lists the identities an identity provider asserted that
// OurWay will not link by itself (2.4-3): a new provider/email pair whose email
// belongs to an existing account. They are accounts with no roles, no password, and
// no login — a decision waiting for an admin, not a queue nobody reads.
//
// GET /api/users/pending (admin)
func (h *UserHandler) ListPendingIdentities(c *gin.Context) {
	users, err := h.store.Users.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list identities"})
		return
	}

	type pendingIdentity struct {
		ID              string    `json:"id"`
		Provider        string    `json:"provider"`
		Subject         string    `json:"subject"`
		Email           string    `json:"email"`
		Name            string    `json:"name,omitempty"`
		EmailVerified   bool      `json:"email_verified"`
		CreatedAt       time.Time `json:"created_at"`
		LinkTargetID    string    `json:"link_target_id,omitempty"`
		LinkTargetUser  string    `json:"link_target_username,omitempty"`
		LinkTargetRoles []string  `json:"link_target_roles,omitempty"`
	}

	out := []pendingIdentity{}
	for _, user := range users {
		if !user.IsPending() {
			continue
		}
		item := pendingIdentity{
			ID: user.ID, Provider: user.PendingProvider, Subject: user.ProviderID,
			Email: user.Email, EmailVerified: emailClaimVerified(user.SSOAttributes),
			CreatedAt: user.CreatedAt, LinkTargetID: user.PendingLinkUserID,
		}
		if target, err := h.store.Users.GetByID(user.PendingLinkUserID); err == nil && target != nil {
			item.LinkTargetUser = target.Username
			if roles, err := h.store.UserRoles.GetUserRoles(target.ID); err == nil {
				for _, r := range roles {
					item.LinkTargetRoles = append(item.LinkTargetRoles, r.Name)
				}
			}
		}
		out = append(out, item)
	}

	c.JSON(http.StatusOK, gin.H{"identities": out})
}

// emailClaimVerified reads the `email_verified` fact out of the stored claim summary.
// It is shown to the admin because "the IdP asserted this email" and "the IdP
// asserted this email is verified" are different things to decide on.
func emailClaimVerified(summary string) bool {
	var record struct {
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := json.Unmarshal([]byte(summary), &record); err != nil {
		return false
	}
	return record.EmailVerified
}

// ApproveIdentity turns a pending SSO identity into its own account at the `viewer`
// floor. The asserted email keeps belonging to whoever it belongs to: approving does
// not merge, and does not copy the target account's roles.
//
// POST /api/users/:id/approve (admin)
func (h *UserHandler) ApproveIdentity(c *gin.Context) {
	id := c.Param("id")

	pending, err := h.store.Users.GetByID(id)
	if err != nil || pending == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "identity not found"})
		return
	}
	if !pending.IsPending() {
		c.JSON(http.StatusConflict, gin.H{"error": "this identity is not pending"})
		return
	}

	defer auditForAnswer(c, h.store, models.AuditUserApprove, AuditTarget{Type: "user", ID: pending.ID}, map[string]any{
		"provider":        pending.PendingProvider,
		"subject":         pending.ProviderID,
		"email":           pending.Email,
		"proposed_target": pending.PendingLinkUserID,
		"granted_role":    "viewer",
	})

	pending.Status = models.UserStatusActive
	pending.PendingLinkUserID = ""
	// `Users.Update` is a full save, and `GetByID` preloaded the role associations:
	// saving the row with them attached writes the join rows a second time. Drop the
	// preloaded associations — this handler changes the account's status, and
	// `grantViewerRole` is the one place its roles are touched.
	pending.Roles = nil
	pending.UserRoles = nil
	if err := h.store.Users.Update(pending); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to approve the identity"})
		return
	}
	if err := h.grantViewerRole(pending.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record the approved role"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "approved", "id": pending.ID, "roles": []string{"viewer"}})
}

// MergeIdentity confirms the match the pending identity claims: the provider's
// identity is attached to the existing account, and the pending row goes away. The
// account's roles are untouched — merging answers *who this identity is*, not what it
// may do, and a merge that also promoted someone would make the admin's confirmation
// mean two things at once.
//
// POST /api/users/:id/merge (admin)
func (h *UserHandler) MergeIdentity(c *gin.Context) {
	id := c.Param("id")

	pending, err := h.store.Users.GetByID(id)
	if err != nil || pending == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "identity not found"})
		return
	}
	if !pending.IsPending() {
		c.JSON(http.StatusConflict, gin.H{"error": "this identity is not pending"})
		return
	}
	target, err := h.store.Users.GetByID(pending.PendingLinkUserID)
	if err != nil || target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "the account this identity points at no longer exists"})
		return
	}

	defer auditForAnswer(c, h.store, models.AuditUserMerge, AuditTarget{Type: "user", ID: target.ID}, map[string]any{
		"identity": pending.ID,
		"provider": pending.PendingProvider,
		"subject":  pending.ProviderID,
		"email":    pending.Email,
		"target":   target.Username,
	})

	target.Provider = pending.PendingProvider
	target.ProviderID = pending.ProviderID
	target.SSOAttributes = pending.SSOAttributes
	// Same full-save trap as the approve path: a merge attaches an identity and
	// touches nothing about the account's roles, so the preloaded associations go.
	target.Roles = nil
	target.UserRoles = nil
	if err := h.store.Users.Update(target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to merge the identity"})
		return
	}
	h.store.UserRoles.SetUserRoles(pending.ID, []string{})
	if err := h.store.Users.Delete(pending.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove the pending record"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "merged", "id": target.ID})
}

// grantViewerRole gives an approved identity the floor role.
func (h *UserHandler) grantViewerRole(userID string) error {
	role, err := h.store.Roles.GetRoleByName("viewer")
	if err != nil {
		return err
	}
	return h.store.UserRoles.AssignRole(userID, role.ID)
}
