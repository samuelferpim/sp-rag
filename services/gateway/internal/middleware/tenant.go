package middleware

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// TenantHeader is the canonical HTTP header that carries the tenant identifier.
const TenantHeader = "X-Tenant-ID"

// TenantLocal is the Fiber Locals key that carries the resolved tenant.
const TenantLocal = "tenant_id"

type tenantCtxKey struct{}

// TenantIDFromContext returns the tenant id previously set on the context by
// one of the middlewares below. Returns empty string if none is set.
func TenantIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(tenantCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// ContextWithTenantID returns a child context carrying the tenant id.
func ContextWithTenantID(parent context.Context, tenantID string) context.Context {
	return context.WithValue(parent, tenantCtxKey{}, tenantID)
}

// IsValidTenantID permits [a-zA-Z0-9_-]{1,64}. Keeping this tight keeps MinIO
// paths, Redis keys, and SpiceDB object IDs consistent and injection-safe.
func IsValidTenantID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// TenantResolver reads X-Tenant-ID from the request header; when present and
// valid, it stores the value on both Fiber Locals and the Go context so
// downstream code can pull it without threading it through every signature.
//
// It does NOT reject requests with a missing header — handlers that accept a
// body/form fallback (upload, query) remain responsible for the final mandatory
// check. An invalid header short-circuits with 400 (fail fast on malformed
// tenant_id; never silently downgrade).
func TenantResolver() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenant := strings.TrimSpace(c.Get(TenantHeader))
		if tenant == "" {
			return c.Next()
		}
		if !IsValidTenantID(tenant) {
			slog.Warn("invalid tenant id in header", "tenant_id", tenant)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "tenant_id must match [a-zA-Z0-9_-]{1,64}",
			})
		}
		c.Locals(TenantLocal, tenant)
		c.SetUserContext(ContextWithTenantID(c.UserContext(), tenant))
		return c.Next()
	}
}
