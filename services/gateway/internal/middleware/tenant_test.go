package middleware

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidTenantID(t *testing.T) {
	cases := []struct {
		name  string
		id    string
		valid bool
	}{
		{"typical", "acme", true},
		{"with_dash", "acme-corp", true},
		{"with_underscore", "acme_corp_01", true},
		{"empty", "", false},
		{"with_slash", "acme/corp", false},
		{"with_colon", "acme:corp", false},
		{"with_space", "acme corp", false},
		{"too_long", string(make([]byte, 65)), false},
		{"boundary_64", string(repeat('a', 64)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.valid, IsValidTenantID(tc.id))
		})
	}
}

func repeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestContextWithTenantID_RoundTrip(t *testing.T) {
	ctx := ContextWithTenantID(context.Background(), "acme")
	assert.Equal(t, "acme", TenantIDFromContext(ctx))

	// Different key types must not collide: a plain string key with the same
	// text as the internal one must NOT leak through.
	ctx2 := context.WithValue(context.Background(), "tenant_id", "other") //nolint:staticcheck
	assert.Equal(t, "", TenantIDFromContext(ctx2),
		"string-keyed context must not shadow the typed tenant key")
}

func TestTenantResolver_ValidHeader_InjectsContext(t *testing.T) {
	app := fiber.New()
	app.Use(TenantResolver())
	app.Get("/echo", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"local": c.Locals(TenantLocal),
			"ctx":   TenantIDFromContext(c.UserContext()),
		})
	})

	req := httptest.NewRequest("GET", "/echo", nil)
	req.Header.Set(TenantHeader, "acme")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	assert.Equal(t, "acme", parsed["local"])
	assert.Equal(t, "acme", parsed["ctx"])
}

func TestTenantResolver_MissingHeader_PassesThrough(t *testing.T) {
	// Missing header is NOT rejected here — handlers may accept body/form
	// fallbacks. We only validate *presence in the header* opportunistically.
	app := fiber.New()
	app.Use(TenantResolver())
	app.Get("/echo", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ctx": TenantIDFromContext(c.UserContext())})
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/echo", nil), -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
}

func TestTenantResolver_InvalidHeader_Returns400(t *testing.T) {
	app := fiber.New()
	app.Use(TenantResolver())
	app.Get("/echo", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/echo", nil)
	req.Header.Set(TenantHeader, "bad/tenant:id")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode)
}
