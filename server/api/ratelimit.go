package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter provides per-user rate limiting using a token bucket.
//
// A plain sliding-window log lets a brand-new key consume its entire
// allowance instantly (a first burst). The token bucket caps the
// instantaneous burst at `burst` tokens while refilling at the sustained
// rate of maxCount per window, so a new client cannot front-run its
// steady-state quota.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	window   time.Duration
	maxCount int
	burst    int
}

type tokenBucket struct {
	tokens     float64
	lastUpdate time.Time
}

// NewRateLimiter creates a new rate limiter allowing maxCount requests
// per window on a sustained basis, with a bounded instantaneous burst.
func NewRateLimiter(window time.Duration, maxCount int) *RateLimiter {
	burst := maxCount / 10
	if burst < 1 {
		burst = 1
	}
	return &RateLimiter{
		buckets:  make(map[string]*tokenBucket),
		window:   window,
		maxCount: maxCount,
		burst:    burst,
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
	b, ok := rl.buckets[key]
	if !ok {
		// New keys start with a full bucket, so the first burst is
		// bounded by `burst` instead of maxCount.
		b = &tokenBucket{tokens: float64(rl.burst), lastUpdate: now}
		rl.buckets[key] = b
	}

	// Refill at maxCount tokens per window.
	rate := float64(rl.maxCount) / rl.window.Seconds()
	b.tokens += now.Sub(b.lastUpdate).Seconds() * rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastUpdate = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Prune drops buckets that have been idle long enough to be refilled
// completely (equivalent to a fresh bucket). Called lazily when the map
// grows large so long-running processes do not leak one bucket per key.
func (rl *RateLimiter) Prune() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	dropped := 0
	for key, b := range rl.buckets {
		if now.Sub(b.lastUpdate) > rl.window {
			delete(rl.buckets, key)
			dropped++
		}
	}
	return dropped
}
