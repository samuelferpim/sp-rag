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

// GetExact looks up an exact cache entry by (tenantID, normalized query, permission hash).
// Returns nil, nil on cache miss.
func (rc *RedisCache) GetExact(ctx context.Context, tenantID, query string, permissions []string) ([]byte, error) {
	key := exactKey(tenantID, query, permissions)

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
func (rc *RedisCache) SetExact(ctx context.Context, tenantID, query string, permissions []string, data []byte) error {
	key := exactKey(tenantID, query, permissions)

	if err := rc.client.Set(ctx, key, data, rc.ttl).Err(); err != nil {
		return fmt.Errorf("exact cache set: %w", err)
	}

	return nil
}

// exactKey builds a deterministic cache key from tenant, normalized query, and permission hash.
// The tenantID is embedded in the plaintext key segment (cache:exact:<tenant>:<hash>) so that
// operators can inspect/invalidate a tenant's cache namespace directly.
func exactKey(tenantID, query string, permissions []string) string {
	// FAIL-SAFE: an empty tenantID at this layer is a programmer error — it
	// means a handler or orchestrator forgot to validate the request. Panic
	// instead of silently colliding everybody's cache into the same keyspace.
	if tenantID == "" {
		panic("cache.exactKey: tenantID is empty — refusing to build a cross-tenant cache key")
	}

	normalized := normalizeQuery(query)
	permHash := permissionHash(permissions)
	// tenantID is included in the hashed material too, as a defense in depth
	// against accidental cross-tenant collisions in downstream tools.
	combined := tenantID + "|" + normalized + "|" + permHash

	h := sha256.Sum256([]byte(combined))
	return exactPrefix + sanitizeTenant(tenantID) + ":" + fmt.Sprintf("%x", h)
}

// sanitizeTenant produces a Redis-safe tenant segment. Keeps alphanumerics, dashes,
// and underscores; everything else becomes '_'. Empty tenant is rejected upstream,
// but we map it to "_" here for safety.
func sanitizeTenant(tenantID string) string {
	if tenantID == "" {
		return "_"
	}
	var b strings.Builder
	b.Grow(len(tenantID))
	for _, r := range tenantID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
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

// scopeTag combines tenant + permission hash into a single RediSearch TAG value.
// Used by the semantic cache to enforce tenant isolation inside KNN queries.
// Panics on empty tenant for the same fail-safe reason as exactKey.
func scopeTag(tenantID string, permissions []string) string {
	if tenantID == "" {
		panic("cache.scopeTag: tenantID is empty — refusing to build a cross-tenant scope tag")
	}
	return sanitizeTenant(tenantID) + "-" + permissionHash(permissions)
}
