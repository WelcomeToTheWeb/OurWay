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

// auditLoginAttempt writes a login record. The actor fields are passed in
// explicitly, because a login has no authenticated context to read: the auth
// middleware runs on the other routes, and the actor here is the account the
// request **claims**, which for a failure may be nobody.
func auditLoginAttempt(c *gin.Context, st *store.Store, action, userID, username string, roles []string, status int) {
	if st == nil || st.Audit == nil {
		return
	}
	row := &models.AuditLog{
		ID:            models.NewAuditID(),
		ActorUserID:   userID,
		ActorUsername: username,
		ActorRoles:    joinRoles(roles),
		Action:        action,
		TargetType:    "route",
		TargetID:      "/api/auth/login",
		Outcome:       models.AuditOutcomeFor(status),
		SourceIP:      c.ClientIP(),
	}
	// Best-effort by construction: an audit row is never the reason a
	// technician cannot log in. A failure here is a server-log line, which is
	// the record of the record's absence.
	_ = st.Audit.Record(row)
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

	defer auditForAnswer(c, h.store, models.AuditAuthRegister, AuditTarget{Type: "user", ID: user.ID}, map[string]any{
		"username": user.Username,
		"role":     roleName,
		"provider": user.Provider,
	})
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

	// Login is recorded on both outcomes, and the failure is the row an MSP
	// reads: `auth.login_failed`, the username that was tried, the source IP,
	// and no user id — the account may not exist. The password is never in
	// the record: the request body is not evidence, and a log of secrets is a
	// second system to breach.
	user, err := h.store.Users.GetByUsername(req.Username)
	if err != nil {
		auditLoginAttempt(c, h.store, models.AuditAuthLoginFailed, "", req.Username, []string{}, http.StatusUnauthorized)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if user.IsPending() {
		// A pending SSO identity (2.4-3) has no password, no roles, and no access. Its
		// password check would fail anyway; naming the reason is what tells the admin
		// who signs in that the row in front of them is a decision to make, not a
		// broken password.
		auditLoginAttempt(c, h.store, models.AuditAuthLoginFailed, user.ID, user.Username, []string{}, http.StatusForbidden)
		c.JSON(http.StatusForbidden, gin.H{"error": "this identity is awaiting an administrator's decision"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		auditLoginAttempt(c, h.store, models.AuditAuthLoginFailed, user.ID, user.Username, []string{}, http.StatusUnauthorized)
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

	// The success row carries the roles the token is about to carry, so the
	// ledger says what the person was allowed to be at that moment.
	auditLoginAttempt(c, h.store, models.AuditAuthLogin, user.ID, user.Username, roleNames, http.StatusOK)
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

	// The password is never recorded; that it changed, on which account, and
	// under which provider is the record.
	defer auditForAnswer(c, h.store, models.AuditAuthPasswordChange, AuditTarget{Type: "user", ID: user.ID}, map[string]any{
		"provider": user.Provider,
	})
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

	// The self-delete is recorded with the id and the username, because after
	// this the account cannot be looked up: "someone deleted an account" with
	// no name is not a record.
	defer auditForAnswer(c, h.store, models.AuditUserDelete, AuditTarget{Type: "user", ID: user.ID}, map[string]any{
		"username": user.Username,
		"self":     true,
	})
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
	// The revoked token id is in the record: "this token stopped working here"
	// is the answer a logout has to give.
	defer auditForAnswer(c, h.store, models.AuditAuthLogout, AuditTarget{Type: "token", ID: claims.ID}, nil)
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
