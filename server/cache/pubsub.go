package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// PubSub provides Redis pub/sub operations for the WebSocket hub.
type PubSub struct {
	redis *RedisClient
}

// NewPubSub creates a new pub/sub wrapper around the Redis client.
func NewPubSub(redis *RedisClient) *PubSub {
	return &PubSub{redis: redis}
}

// Publish publishes a message to a channel.
func (p *PubSub) Publish(channel string, message []byte) error {
	ctx := context.Background()
	_, err := p.redis.GetClient().Publish(ctx, channel, string(message)).Result()
	if err != nil {
		return fmt.Errorf("failed to publish to %s: %w", channel, err)
	}
	return nil
}

// Subscribe subscribes to a channel and calls the handler for each message.
// Blocks until the context is cancelled.
func (p *PubSub) Subscribe(channel string, handler func([]byte)) error {
	ctx := context.Background()
	pubsub := p.redis.GetClient().Subscribe(ctx, channel)
	defer pubsub.Close()

	log.Printf("cache: subscribed to channel %s", channel)

	for {
		select {
		case msg := <-pubsub.Channel():
			handler([]byte(msg.Payload))
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// MarshalJSON marshals a value to JSON bytes.
func MarshalJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
