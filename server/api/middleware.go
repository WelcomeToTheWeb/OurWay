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

		c.JSON(403, gin.H{"error": "insufficient permissions"})
		c.Abort()
	}
}
