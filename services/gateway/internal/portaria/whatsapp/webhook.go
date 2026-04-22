// Package whatsapp adapts the Meta WhatsApp Cloud API to the channel-agnostic
// portaria.Service.
//
// Two Fiber routes:
//   - GET  /webhook/whatsapp  → Meta verification handshake
//   - POST /webhook/whatsapp  → inbound messages (HMAC-verified)
package whatsapp

import (
	"context"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"sp-rag-gateway/internal/portaria"
)

// Config is what main.go populates from env. A webhook without VerifyToken,
// AppSecret, AccessToken, or a non-empty TenantByPhoneID map is a
// misconfiguration — the Handler's constructor refuses to build one.
type Config struct {
	VerifyToken string
	AppSecret   string
	AccessToken string
	// TenantByPhoneID maps the Meta phone_number_id (receiving number) to
	// our internal tenant_id. One condo == one phone_number_id == one tenant.
	TenantByPhoneID map[string]string
}

type Handler struct {
	cfg    Config
	svc    *portaria.Service
	sender Sender
}

func NewHandler(cfg Config, svc *portaria.Service, sender Sender) *Handler {
	return &Handler{cfg: cfg, svc: svc, sender: sender}
}

// Register mounts both webhook routes.
func (h *Handler) Register(app *fiber.App) {
	app.Get("/webhook/whatsapp", h.Verify)
	app.Post("/webhook/whatsapp", h.Receive)
}

// Verify implements the Meta handshake: the webhook URL is called with
// hub.mode=subscribe and a token that must match our VerifyToken. On success
// we echo hub.challenge back. Meta uses this to prove we own the endpoint.
func (h *Handler) Verify(c *fiber.Ctx) error {
	mode := c.Query("hub.mode")
	token := c.Query("hub.verify_token")
	challenge := c.Query("hub.challenge")

	if mode == "subscribe" && token == h.cfg.VerifyToken && h.cfg.VerifyToken != "" {
		return c.SendString(challenge)
	}
	return c.SendStatus(fiber.StatusForbidden)
}

// Meta's inbound payload (trimmed to fields we care about).
// Full schema: https://developers.facebook.com/docs/whatsapp/cloud-api/webhooks/payload-examples
type webhookPayload struct {
	Object string  `json:"object"`
	Entry  []entry `json:"entry"`
}
type entry struct {
	ID      string   `json:"id"`
	Changes []change `json:"changes"`
}
type change struct {
	Field string `json:"field"`
	Value value  `json:"value"`
}
type value struct {
	MessagingProduct string        `json:"messaging_product"`
	Metadata         metadata      `json:"metadata"`
	Messages         []wppMessage  `json:"messages"`
	Contacts         []wppContact  `json:"contacts"`
	Statuses         []any         `json:"statuses"`
}
type metadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}
type wppContact struct {
	WaID    string `json:"wa_id"`
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
}
type wppMessage struct {
	From      string `json:"from"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Text      struct {
		Body string `json:"body"`
	} `json:"text"`
}

// Receive handles inbound user messages.
//
// Ordering is critical:
//  1. HMAC the raw body BEFORE parsing — a tampered body with a matching
//     "object" field should still be rejected.
//  2. Resolve tenant from phone_number_id (metadata). Unknown phone_number_id
//     means this webhook is probably shared with another product; ignore it
//     with 200 so Meta doesn't retry.
//  3. Hand off to Service. Reply is sent via Sender out-of-band; we ack Meta
//     with 200 immediately to avoid webhook retries.
//
// We ALWAYS return 200 on parseable-but-unusable payloads (unknown phone,
// non-text message, etc.) because Meta retries aggressively on non-2xx and
// that would amplify noise. Real errors (bad signature) return 401.
func (h *Handler) Receive(c *fiber.Ctx) error {
	body := c.Body()

	if !ValidateSignature(h.cfg.AppSecret, c.Get("X-Hub-Signature-256"), body) {
		slog.Warn("whatsapp: signature validation failed")
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	var payload webhookPayload
	if err := c.BodyParser(&payload); err != nil {
		slog.Warn("whatsapp: bad json", "error", err)
		return c.SendStatus(fiber.StatusBadRequest)
	}

	for _, e := range payload.Entry {
		for _, ch := range e.Changes {
			if ch.Field != "messages" {
				continue
			}
			phoneID := ch.Value.Metadata.PhoneNumberID
			tenantID, ok := h.cfg.TenantByPhoneID[phoneID]
			if !ok {
				slog.Warn("whatsapp: unknown phone_number_id, ignoring",
					"phone_number_id", phoneID)
				continue
			}
			for _, msg := range ch.Value.Messages {
				h.handleInbound(c.UserContext(), tenantID, phoneID, msg)
			}
		}
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *Handler) handleInbound(ctx context.Context, tenantID, phoneNumberID string, msg wppMessage) {
	if msg.Type != "text" || msg.Text.Body == "" {
		slog.Info("whatsapp: non-text message skipped",
			"tenant_id", tenantID, "type", msg.Type, "wa_message_id", msg.ID)
		return
	}

	reply, err := h.svc.HandleMessage(ctx, tenantID, msg.From, msg.Text.Body)
	if err != nil {
		// Service guarantees a non-nil reply even on error; keep going.
		slog.Error("whatsapp: service error, sending fallback",
			"error", err, "tenant_id", tenantID, "sender", msg.From)
	}
	if reply == nil {
		return
	}

	if sendErr := h.sender.SendText(ctx, phoneNumberID, msg.From, reply.Text); sendErr != nil {
		slog.Error("whatsapp: failed to send reply",
			"error", sendErr,
			"tenant_id", tenantID,
			"to", msg.From,
			"phone_number_id", phoneNumberID,
		)
		return
	}

	slog.Info("whatsapp: reply sent",
		"tenant_id", tenantID,
		"to", msg.From,
		"routed", reply.Routed,
		"cached", reply.Cached,
		"grounded", reply.Grounded,
	)
}
