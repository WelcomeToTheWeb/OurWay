package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"ourway/server/cache"
)

// tokenBucketScript is a Lua token-bucket: refills at `rate` tokens/sec up
// to `capacity`, starts new keys full, and atomically consumes one token.
// This mirrors the in-memory RateLimiter so both backends share the
// bounded-burst, sustained-rate semantics (no first-contact burst).
const tokenBucketScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil then
  tokens = capacity
  ts = now
end
tokens = math.min(capacity, tokens + (now - ts) * rate / 1000000000)
local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end
redis.call('HMSET', key, 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', key, math.ceil(capacity / rate * 1000) + 1000)
return allowed
`

// RedisRateLimiter provides distributed rate limiting using Redis.
type RedisRateLimiter struct {
	rc       *cache.RedisClient
	window   time.Duration
	maxCount int
	burst    int
}

// NewRedisRateLimiter creates a new Redis-backed rate limiter.
func NewRedisRateLimiter(rc *cache.RedisClient, window time.Duration, maxCount int) *RedisRateLimiter {
	burst := maxCount / 10
	if burst < 1 {
		burst = 1
	}
	return &RedisRateLimiter{
		rc:       rc,
		window:   window,
		maxCount: maxCount,
		burst:    burst,
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

		rate := float64(rl.maxCount) / rl.window.Seconds()
		allowed, err := rl.rc.GetClient().Eval(
			context.Background(),
			tokenBucketScript,
			[]string{redisKey},
			rl.burst,
			rate,
			time.Now().UnixNano(),
		).Int()
		if err != nil {
			// Redis error, allow the request (fail open)
			c.Next()
			return
		}

		if allowed != 1 {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

var _ = redis.Nil // keep redis import for future typed-error handling
