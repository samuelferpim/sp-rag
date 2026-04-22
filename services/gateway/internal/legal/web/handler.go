// Package web mounts the Legal vertical's chat UI and JSON endpoint.
//
// Two routes:
//   - GET  /legal                 → embedded chat UI (for lawyers)
//   - POST /api/v1/legal/query    → JSON in/out (same endpoint as the REST API
//                                   package; the web UI calls this directly)
//
// The JSON endpoint is shared with the REST API package intentionally — one
// route, one contract. The web and API channels only differ in presentation.
package web

import (
	_ "embed"

	"github.com/gofiber/fiber/v2"

	"sp-rag-gateway/internal/legal"
	"sp-rag-gateway/internal/middleware"
)

//go:embed chat.html
var chatHTML []byte

type Handler struct {
	Service *legal.Service
}

func NewHandler(svc *legal.Service) *Handler {
	return &Handler{Service: svc}
}

type queryRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Query    string `json:"query"`
}

// Register wires both routes on app. The JSON endpoint is registered here
// (not in a separate package) so both web and any other JSON client hit the
// exact same handler, guaranteeing contract parity.
func (h *Handler) Register(app *fiber.App, apiPrefix string) {
	app.Get("/legal", h.serveUI)
	app.Post(apiPrefix+"/legal/query", h.Query)
}

func (h *Handler) serveUI(c *fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.Send(chatHTML)
}

func (h *Handler) Query(c *fiber.Ctx) error {
	var req queryRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	if ctxTenant := middleware.TenantIDFromContext(c.UserContext()); ctxTenant != "" {
		req.TenantID = ctxTenant
	}
	if req.TenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id is required"})
	}
	if !middleware.IsValidTenantID(req.TenantID) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tenant_id must match [a-zA-Z0-9_-]{1,64}"})
	}
	if req.UserID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "user_id is required"})
	}
	if req.Query == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "query is required"})
	}

	reply, err := h.Service.HandleQuery(c.UserContext(), req.TenantID, req.UserID, req.Query)
	if err != nil {
		// Service always returns a non-nil reply on error with a user-safe
		// message. Propagate it with 502 so callers can retry or alert.
		if reply != nil {
			return c.Status(fiber.StatusBadGateway).JSON(reply)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}
	return c.JSON(reply)
}
