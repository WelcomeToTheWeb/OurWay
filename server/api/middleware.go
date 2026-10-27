package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"ourway/server/auth"
)

// AuthMiddleware validates the JWT token and sets user claims in context.
func AuthMiddleware(jwtAuth *auth.JWTAuth) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(401, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := jwtAuth.ValidateToken(token)
		if err != nil {
			c.JSON(401, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("roles", claims.Roles)
		c.Next()
	}
}

// hasRole reports whether the authenticated user on this context holds
// the given role. Used by handlers that need a role-based branch
// (e.g. ListSessions) instead of a middleware that aborts.
func hasRole(c *gin.Context, role string) bool {
	roles, exists := c.Get("roles")
	if !exists {
		return false
	}
	userRoles, ok := roles.([]string)
	if !ok {
		return false
	}
	for _, r := range userRoles {
		if r == role {
			return true
		}
	}
	return false
}

// auditRoleDenial records a role refusal.
//
// The RBAC middlewares run **before** any handler, so the handler-side writer
// never sees the most important class of refusal in an MSP product: a viewer
// pressing delete on a unit, a technician calling an admin route. Nothing in
// the handler runs, so nothing there can record it — and a ledger with no
// denied attempts is a ledger of what the installation allowed.
//
// The middleware knows the actor (the auth middleware loaded it), the action
// (the registry, keyed on the route the router matched) and the status. It does
// not know the target's client, because no lookup happened: that is the
// handler's knowledge, so the row says what the middleware knows.
func auditRoleDenial(c *gin.Context, status int, reason string) {
	st := auditStoreFromContext(c)
	if st == nil {
		return
	}
	action, ok := lookupAuditAction(c.Request.Method, c.FullPath())
	if !ok {
		return
	}
	target := AuditTarget{Type: "route", ID: c.FullPath()}
	if id := c.Param("id"); id != "" {
		target.ID = id
		target.Type = "entity"
	}
	recordAudit(c, st, action, status, target, map[string]any{
		"denied": reason,
		"route":  c.FullPath(),
		"path":   c.Request.URL.Path,
	})
}

// RequireRole checks that the authenticated user has the specified role.
func RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		roles, exists := c.Get("roles")
		if !exists {
			c.JSON(401, gin.H{"error": "not authenticated"})
			c.Abort()
			return
		}

		userRoles, ok := roles.([]string)
		if !ok {
			c.JSON(401, gin.H{"error": "invalid roles format"})
			c.Abort()
			return
		}

		for _, r := range userRoles {
			if r == role {
				c.Next()
				return
			}
		}

		auditRoleDenial(c, 403, "requires role "+role)
		c.JSON(403, gin.H{"error": "insufficient permissions"})
		c.Abort()
	}
}

// RequireAnyRole checks that the authenticated user has at least one of the specified roles.
func RequireAnyRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRoles, exists := c.Get("roles")
		if !exists {
			c.JSON(401, gin.H{"error": "not authenticated"})
			c.Abort()
			return
		}

		ur, ok := userRoles.([]string)
		if !ok {
			c.JSON(401, gin.H{"error": "invalid roles format"})
			c.Abort()
			return
		}

		for _, r := range ur {
			for _, needed := range roles {
				if r == needed {
					c.Next()
					return
				}
			}
		}

		auditRoleDenial(c, 403, "requires one of: "+strings.Join(roles, ", "))
		c.JSON(403, gin.H{"error": "insufficient permissions"})
		c.Abort()
	}
}
