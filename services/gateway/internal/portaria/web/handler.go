// Package web exposes the Portaria Service via HTTP + an embedded chat UI.
// This is the "admin/demo" channel — WhatsApp is the real production surface.
package web

import (
	_ "embed"

	"github.com/gofiber/fiber/v2"

	"sp-rag-gateway/internal/middleware"
	"sp-rag-gateway/internal/portaria"
)

//go:embed chat.html
var chatHTML []byte

// Handler wires the Portaria Service to two Fiber routes:
//   - POST /api/v1/portaria/chat   → JSON in/out
//   - GET  /portaria               → embedded chat UI (for demo + admin)
type Handler struct {
	Service *portaria.Service
}

func NewHandler(svc *portaria.Service) *Handler {
	return &Handler{Service: svc}
}

type chatRequest struct {
	TenantID string `json:"tenant_id"`
	SenderID string `json:"sender_id"`
	Message  string `json:"message"`
}

// Register mounts both routes on the app.
func (h *Handler) Register(app *fiber.App, apiPrefix string) {
	app.Get("/portaria", h.serveUI)
	app.Post(apiPrefix+"/portaria/chat", h.Chat)
}

func (h *Handler) serveUI(c *fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.Send(chatHTML)
}

// Chat accepts {sender_id, message} and returns a Reply. Tenant comes from
// X-Tenant-ID (middleware) with body fallback for thin clients.
func (h *Handler) Chat(c *fiber.Ctx) error {
	var req chatRequest
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
	if req.SenderID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "sender_id is required"})
	}
	if req.Message == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "message is required"})
	}

	reply, err := h.Service.HandleMessage(c.UserContext(), req.TenantID, req.SenderID, req.Message)
	if err != nil {
		// Service contract: reply is non-nil even on error. Surface the
		// friendly copy but signal the failure with 502 so clients can alert.
		if reply != nil {
			return c.Status(fiber.StatusBadGateway).JSON(reply)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal error"})
	}
	return c.JSON(reply)
}
