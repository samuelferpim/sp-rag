package handler

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"

	"sp-rag-gateway/internal/orchestrator"
)

type QueryRequest struct {
	Query  string `json:"query"`
	UserID string `json:"user_id"`
	TopK   int    `json:"top_k,omitempty"`
}

func (h *Handler) Query(c *fiber.Ctx) error {
	var req QueryRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
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

	result, err := h.Orchestrator.Execute(ctx, req.Query, req.UserID, topK)
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
