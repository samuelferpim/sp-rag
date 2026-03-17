package handler

import (
	"github.com/minio/minio-go/v7"
	"github.com/segmentio/kafka-go"

	"sp-rag-gateway/internal/authz"
	"sp-rag-gateway/internal/config"
	"sp-rag-gateway/internal/orchestrator"
)

type Handler struct {
	Config       *config.Config
	MinIO        *minio.Client
	KafkaWriter  *kafka.Writer
	Authz        *authz.AuthzClient
	Orchestrator *orchestrator.QueryOrchestrator
}
