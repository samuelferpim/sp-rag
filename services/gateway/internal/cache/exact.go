package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/redis/go-redis/v9"
)

const exactPrefix = "cache:exact:"

// GetExact looks up an exact cache entry by SHA-256(normalized query + permission hash).
// Returns nil, nil on cache miss.
func (rc *RedisCache) GetExact(ctx context.Context, query string, permissions []string) ([]byte, error) {
	key := exactKey(query, permissions)

	data, err := rc.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("exact cache get: %w", err)
	}

	return data, nil
}

// SetExact stores a response in the exact cache with the configured TTL.
func (rc *RedisCache) SetExact(ctx context.Context, query string, permissions []string, data []byte) error {
	key := exactKey(query, permissions)

	if err := rc.client.Set(ctx, key, data, rc.ttl).Err(); err != nil {
		return fmt.Errorf("exact cache set: %w", err)
	}

	return nil
}

// exactKey builds a deterministic cache key from the normalized query and permission hash.
func exactKey(query string, permissions []string) string {
	normalized := normalizeQuery(query)
	permHash := permissionHash(permissions)
	combined := normalized + "|" + permHash

	h := sha256.Sum256([]byte(combined))
	return exactPrefix + fmt.Sprintf("%x", h)
}

// normalizeQuery lowercases, trims whitespace, and strips extra punctuation.
func normalizeQuery(query string) string {
	q := strings.ToLower(strings.TrimSpace(query))

	// Collapse multiple spaces
	var b strings.Builder
	prevSpace := false
	for _, r := range q {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			b.WriteRune(r)
			prevSpace = false
		}
	}

	return strings.TrimSpace(b.String())
}

// permissionHash returns a deterministic SHA-256 hash of sorted permissions.
func permissionHash(permissions []string) string {
	sorted := make([]string, len(permissions))
	copy(sorted, permissions)
	sort.Strings(sorted)

	h := sha256.Sum256([]byte(strings.Join(sorted, "|")))
	return fmt.Sprintf("%x", h)
}
