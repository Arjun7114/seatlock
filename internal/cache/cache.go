package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache wraps a Redis client with the operations seatlock needs.
type Cache struct {
	client *redis.Client
}

// New connects to Redis and verifies the connection.
func New(ctx context.Context, addr string) (*Cache, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Cache{client: client}, nil
}
// Allow implements a fixed-window rate limiter. It increments a counter for
// `key` and returns false once the count exceeds `limit` within `window`.
// Best-effort: on a Redis error it fails OPEN (allows the request) so the
// limiter can never take down the service.
func (c *Cache) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	count, err := c.client.Incr(ctx, key).Result()
	if err != nil {
		return true, err // fail open
	}
	// First hit in this window — set the expiry.
	if count == 1 {
		_ = c.client.Expire(ctx, key, window).Err()
	}
	return count <= int64(limit), nil
}

// Get returns the cached value for key, or redis.Nil error if absent.
func (c *Cache) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

// Set stores a value with a TTL.
func (c *Cache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes a key (used to invalidate on booking changes).
func (c *Cache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

// Close releases the connection.
func (c *Cache) Close() error {
	return c.client.Close()
}