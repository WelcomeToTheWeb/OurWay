package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter provides per-user rate limiting using a sliding window.
type RateLimiter struct {
	mu       sync.Mutex
	entries  map[string][]time.Time
	window   time.Duration
	maxCount int
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(window time.Duration, maxCount int) *RateLimiter {
	return &RateLimiter{
		entries:  make(map[string][]time.Time),
		window:   window,
		maxCount: maxCount,
	}
}

// Middleware returns a Gin middleware that enforces rate limits per user.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user ID from context, or fall back to IP
		key := c.ClientIP()
		if userID, exists := c.Get("user_id"); exists {
			key = "user:" + userID.(string)
		}

		if !rl.Allow(key) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Allow checks if a request is allowed for the given key.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Remove old entries
	var recent []time.Time
	for _, t := range rl.entries[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.maxCount {
		rl.entries[key] = recent
		return false
	}

	recent = append(recent, now)
	rl.entries[key] = recent
	return true
}
