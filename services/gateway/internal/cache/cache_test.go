package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase", "What Is RAG?", "what is rag"},
		{"trim spaces", "  hello world  ", "hello world"},
		{"collapse spaces", "hello   world", "hello world"},
		{"remove punctuation", "what's the revenue?!", "whats the revenue"},
		{"mixed", "  What  IS  the  Revenue??  ", "what is the revenue"},
		{"empty", "", ""},
		{"only spaces", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeQuery(tt.input))
		})
	}
}

func TestPermissionHash(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		h1 := permissionHash([]string{"finance_team", "admin"})
		h2 := permissionHash([]string{"finance_team", "admin"})
		assert.Equal(t, h1, h2)
	})

	t.Run("order independent", func(t *testing.T) {
		h1 := permissionHash([]string{"admin", "finance_team"})
		h2 := permissionHash([]string{"finance_team", "admin"})
		assert.Equal(t, h1, h2, "sorted permissions should produce same hash")
	})

	t.Run("different permissions different hash", func(t *testing.T) {
		h1 := permissionHash([]string{"finance_team"})
		h2 := permissionHash([]string{"eng_team"})
		assert.NotEqual(t, h1, h2)
	})

	t.Run("empty permissions", func(t *testing.T) {
		h1 := permissionHash([]string{})
		h2 := permissionHash([]string{})
		assert.Equal(t, h1, h2)
	})

	t.Run("nil permissions", func(t *testing.T) {
		h := permissionHash(nil)
		assert.NotEmpty(t, h)
	})
}

func TestExactKey(t *testing.T) {
	t.Run("same inputs same key", func(t *testing.T) {
		k1 := exactKey("what is rag?", []string{"finance_team"})
		k2 := exactKey("what is rag?", []string{"finance_team"})
		assert.Equal(t, k1, k2)
	})

	t.Run("different permissions different key", func(t *testing.T) {
		k1 := exactKey("what is rag?", []string{"finance_team"})
		k2 := exactKey("what is rag?", []string{"eng_team"})
		assert.NotEqual(t, k1, k2, "different permissions must produce different cache keys")
	})

	t.Run("normalized query same key", func(t *testing.T) {
		k1 := exactKey("What is RAG?", []string{"admin"})
		k2 := exactKey("what is rag", []string{"admin"})
		assert.Equal(t, k1, k2, "normalization should make these equivalent")
	})

	t.Run("has prefix", func(t *testing.T) {
		k := exactKey("test", []string{})
		assert.Contains(t, k, exactPrefix)
	})

	t.Run("is sha256 hex", func(t *testing.T) {
		k := exactKey("test", []string{"a"})
		hash := k[len(exactPrefix):]
		assert.Len(t, hash, 64, "SHA-256 hex should be 64 chars")
	})
}

func TestPermissionIsolation(t *testing.T) {
	// Core security test: same query with different permissions MUST produce different keys.
	// Without this, user A could see cached results from user B.
	query := "what are the financial results?"

	aliceKey := exactKey(query, []string{"finance_team", "hr_team"})
	bobKey := exactKey(query, []string{"eng_team"})
	charlieKey := exactKey(query, []string{"eng_team", "finance_team"})

	assert.NotEqual(t, aliceKey, bobKey, "alice and bob have different permissions")
	assert.NotEqual(t, aliceKey, charlieKey, "alice and charlie have different permissions")
	assert.NotEqual(t, bobKey, charlieKey, "bob and charlie have different permissions")
}
