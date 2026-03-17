package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/segmentio/kafka-go"
)

type UploadResponse struct {
	DocumentID string `json:"document_id"`
	FileName   string `json:"file_name"`
	FilePath   string `json:"file_path"`
	Message    string `json:"message"`
	Status     string `json:"status"`
}

type DocumentUploadedEvent struct {
	FilePath    string   `json:"file_path"`
	FileName    string   `json:"file_name"`
	UserID      string   `json:"user_id"`
	Permissions []string `json:"permissions"`
	UploadedAt  string   `json:"uploaded_at"`
}

func (h *Handler) Upload(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "file is required",
		})
	}

	userID := c.FormValue("user_id")
	if userID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "user_id is required",
		})
	}

	permissionsRaw := c.FormValue("permissions")
	var permissions []string
	if permissionsRaw != "" {
		permissions = strings.Split(permissionsRaw, ",")
		for i := range permissions {
			permissions[i] = strings.TrimSpace(permissions[i])
		}
	}

	docID := uuid.New().String()
	filePath := fmt.Sprintf("documents/%s", file.Filename)

	src, err := file.Open()
	if err != nil {
		slog.Error("failed to open uploaded file", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to read uploaded file",
		})
	}
	defer src.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = h.MinIO.PutObject(ctx, h.Config.MinIOBucket, filePath, src, file.Size, minio.PutObjectOptions{
		ContentType: "application/pdf",
	})
	if err != nil {
		slog.Error("failed to upload to MinIO", "error", err, "file_path", filePath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to store document",
		})
	}

	event := DocumentUploadedEvent{
		FilePath:    filePath,
		FileName:    file.Filename,
		UserID:      userID,
		Permissions: permissions,
		UploadedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	eventBytes, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal kafka event", "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to queue document for processing",
		})
	}

	err = h.KafkaWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(filePath),
		Value: eventBytes,
	})
	if err != nil {
		slog.Error("failed to publish kafka event", "error", err, "file_path", filePath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to queue document for processing",
		})
	}

	// Create SpiceDB relationships: owner + viewer teams
	if err := h.Authz.CreateDocumentRelationships(ctx, filePath, userID, permissions); err != nil {
		slog.Error("failed to create spicedb relationships", "error", err, "file_path", filePath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to register document permissions",
		})
	}

	slog.Info("document uploaded and queued",
		"document_id", docID,
		"file_name", file.Filename,
		"file_path", filePath,
		"user_id", userID,
		"permissions", permissions,
	)

	return c.Status(fiber.StatusAccepted).JSON(UploadResponse{
		DocumentID: docID,
		FileName:   file.Filename,
		FilePath:   filePath,
		Message:    "Document uploaded and queued for processing",
		Status:     "processing",
	})
}
