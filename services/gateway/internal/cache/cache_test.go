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
		k1 := exactKey("acme", "what is rag?", []string{"finance_team"})
		k2 := exactKey("acme", "what is rag?", []string{"finance_team"})
		assert.Equal(t, k1, k2)
	})

	t.Run("different permissions different key", func(t *testing.T) {
		k1 := exactKey("acme", "what is rag?", []string{"finance_team"})
		k2 := exactKey("acme", "what is rag?", []string{"eng_team"})
		assert.NotEqual(t, k1, k2, "different permissions must produce different cache keys")
	})

	t.Run("normalized query same key", func(t *testing.T) {
		k1 := exactKey("acme", "What is RAG?", []string{"admin"})
		k2 := exactKey("acme", "what is rag", []string{"admin"})
		assert.Equal(t, k1, k2, "normalization should make these equivalent")
	})

	t.Run("has prefix", func(t *testing.T) {
		k := exactKey("acme", "test", []string{})
		assert.Contains(t, k, exactPrefix)
	})

	t.Run("tenant segment in key", func(t *testing.T) {
		k := exactKey("acme", "test", []string{})
		assert.Contains(t, k, exactPrefix+"acme:", "tenant must appear in key namespace")
	})
}

func TestTenantIsolation(t *testing.T) {
	// Core multi-tenant security test: the SAME query with the SAME permissions
	// MUST produce different keys across tenants. Without this, tenant A could
	// read tenant B's cached responses.
	query := "what are the financial results?"
	perms := []string{"finance_team"}

	acme := exactKey("acme", query, perms)
	contoso := exactKey("contoso", query, perms)
	initech := exactKey("initech", query, perms)

	assert.NotEqual(t, acme, contoso, "tenants acme and contoso must not share cache keys")
	assert.NotEqual(t, acme, initech, "tenants acme and initech must not share cache keys")
	assert.NotEqual(t, contoso, initech, "tenants contoso and initech must not share cache keys")
}

func TestPermissionIsolation(t *testing.T) {
	// Within a single tenant: same query with different permissions MUST produce different keys.
	// Without this, user A could see cached results from user B.
	query := "what are the financial results?"

	aliceKey := exactKey("acme", query, []string{"finance_team", "hr_team"})
	bobKey := exactKey("acme", query, []string{"eng_team"})
	charlieKey := exactKey("acme", query, []string{"eng_team", "finance_team"})

	assert.NotEqual(t, aliceKey, bobKey, "alice and bob have different permissions")
	assert.NotEqual(t, aliceKey, charlieKey, "alice and charlie have different permissions")
	assert.NotEqual(t, bobKey, charlieKey, "bob and charlie have different permissions")
}

func TestScopeTag(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		a := scopeTag("acme", []string{"finance_team"})
		b := scopeTag("acme", []string{"finance_team"})
		assert.Equal(t, a, b)
	})

	t.Run("differs across tenants", func(t *testing.T) {
		a := scopeTag("acme", []string{"finance_team"})
		b := scopeTag("contoso", []string{"finance_team"})
		assert.NotEqual(t, a, b, "scope tag must isolate tenants in the semantic index")
	})

	t.Run("differs across permission sets", func(t *testing.T) {
		a := scopeTag("acme", []string{"finance_team"})
		b := scopeTag("acme", []string{"eng_team"})
		assert.NotEqual(t, a, b)
	})
}

func TestSanitizeTenant(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"acme", "acme"},
		{"Acme-42_x", "Acme-42_x"},
		{"acme corp", "acme_corp"},
		{"a.b/c", "a_b_c"},
		{"", "_"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeTenant(tt.in))
		})
	}
}
