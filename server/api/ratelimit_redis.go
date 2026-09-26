package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ourway/server/cache"
)

// RedisRateLimiter provides distributed rate limiting using Redis.
type RedisRateLimiter struct {
	rc       *cache.RedisClient
	window   time.Duration
	maxCount int
}

// NewRedisRateLimiter creates a new Redis-backed rate limiter.
func NewRedisRateLimiter(rc *cache.RedisClient, window time.Duration, maxCount int) *RedisRateLimiter {
	return &RedisRateLimiter{
		rc:       rc,
		window:   window,
		maxCount: maxCount,
	}
}

// Middleware returns a Gin middleware that enforces rate limits per user via Redis.
func (rl *RedisRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user ID from context, or fall back to IP
		key := c.ClientIP()
		if userID, exists := c.Get("user_id"); exists {
			key = "user:" + userID.(string)
		}

		redisKey := "ratelimit:" + key

		count, err := rl.rc.Incr(redisKey, rl.window)
		if err != nil {
			// Redis error, allow the request (fail open)
			c.Next()
			return
		}

		if count > int64(rl.maxCount) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
