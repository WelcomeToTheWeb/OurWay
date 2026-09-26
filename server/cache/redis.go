package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisClient wraps the Redis client with convenience methods.
type RedisClient struct {
	client *redis.Client
}

// NewRedisClient creates a new Redis client from a URL.
func NewRedisClient(ctx context.Context, url string) (*RedisClient, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping Redis: %w", err)
	}

	return &RedisClient{client: client}, nil
}

// Set stores a value with an expiration.
func (r *RedisClient) Set(key string, value interface{}, expiration time.Duration) error {
	val, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), key, string(val), expiration).Err()
}

// Get retrieves a value by key.
func (r *RedisClient) Get(key string, out interface{}) error {
	val, err := r.client.Get(context.Background(), key).Result()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("key not found: %s", key)
		}
		return err
	}
	return json.Unmarshal([]byte(val), out)
}

// Delete removes a key.
func (r *RedisClient) Delete(key string) error {
	return r.client.Del(context.Background(), key).Err()
}

// Incr increments a counter and sets expiration if new.
func (r *RedisClient) Incr(key string, expiration time.Duration) (int64, error) {
	ctx := context.Background()
	count, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	// Set expiration if this is the first increment
	if count == 1 {
		r.client.Expire(ctx, key, expiration)
	}
	return count, nil
}

// Close closes the Redis connection.
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// GetClient returns the underlying redis client for advanced operations.
func (r *RedisClient) GetClient() *redis.Client {
	return r.client
}
