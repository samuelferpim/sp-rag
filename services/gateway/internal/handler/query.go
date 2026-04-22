package handler

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"

	"sp-rag-gateway/internal/middleware"
	"sp-rag-gateway/internal/orchestrator"
)

type QueryRequest struct {
	TenantID string `json:"tenant_id"`
	Query    string `json:"query"`
	UserID   string `json:"user_id"`
	TopK     int    `json:"top_k,omitempty"`
}

func (h *Handler) Query(c *fiber.Ctx) error {
	var req QueryRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	// Precedence: Go context (set by TenantResolver from X-Tenant-ID header)
	// → explicit body field. Keeping the body fallback lets thin clients skip
	// the header, but the middleware path is preferred for service-to-service.
	if ctxTenant := middleware.TenantIDFromContext(c.UserContext()); ctxTenant != "" {
		req.TenantID = ctxTenant
	}

	if req.TenantID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "tenant_id is required",
		})
	}
	if !middleware.IsValidTenantID(req.TenantID) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "tenant_id must match [a-zA-Z0-9_-]{1,64}",
		})
	}
	if req.Query == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "query is required",
		})
	}
	if req.UserID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "user_id is required",
		})
	}

	topK := req.TopK
	if topK <= 0 {
		topK = h.Config.QueryTopK
	}

	timeout := time.Duration(h.Config.QueryTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(c.UserContext(), timeout)
	defer cancel()

	result, err := h.Orchestrator.Execute(ctx, req.TenantID, req.Query, req.UserID, topK)
	if err != nil {
		var qe *orchestrator.QueryError
		if errors.As(err, &qe) {
			return c.Status(qe.StatusCode).JSON(fiber.Map{
				"error": qe.Message,
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "internal error",
		})
	}

	return c.JSON(result)
}
