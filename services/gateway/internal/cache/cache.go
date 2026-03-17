package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache defines the interface for query caching.
// All lookups are permission-aware to prevent data leakage across scopes.
type Cache interface {
	// Exact cache: SHA-256 of (normalized query + permission hash)
	GetExact(ctx context.Context, query string, permissions []string) ([]byte, error)
	SetExact(ctx context.Context, query string, permissions []string, data []byte) error

	// Semantic cache: vector similarity search filtered by permission hash
	GetSemantic(ctx context.Context, queryVector []float32, permissions []string) ([]byte, float64, error)
	SetSemantic(ctx context.Context, queryVector []float32, permissions []string, data []byte) error

	// EnsureIndex creates the RediSearch vector index if it doesn't exist.
	EnsureIndex(ctx context.Context) error
}

// RedisCache implements Cache using Redis (exact hash) and RediSearch (semantic vectors).
type RedisCache struct {
	client    *redis.Client
	ttl       time.Duration
	threshold float64
	dims      int
}

// NewRedisCache creates a RedisCache connected to the given Redis instance.
func NewRedisCache(addr, password string, db int, ttl time.Duration, threshold float64, dims int) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		UnstableResp3: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &RedisCache{
		client:    client,
		ttl:       ttl,
		threshold: threshold,
		dims:      dims,
	}, nil
}

// Close closes the underlying Redis connection.
func (rc *RedisCache) Close() error {
	return rc.client.Close()
}
