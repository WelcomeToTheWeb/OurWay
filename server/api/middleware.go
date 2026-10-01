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

// DeviceKeyMiddleware validates the device API key from the X-Device-Key header.
func DeviceKeyMiddleware(store interface {
	GetDeviceByKey(string) (interface{}, error)
}) gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceKey := c.GetHeader("X-Device-Key")
		if deviceKey == "" {
			c.JSON(401, gin.H{"error": "missing device key"})
			c.Abort()
			return
		}
		c.Set("device_key", deviceKey)
		c.Next()
	}
}
