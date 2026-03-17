package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/qdrant/go-client/qdrant"
	openai "github.com/sashabaranov/go-openai"
	"github.com/segmentio/kafka-go"

	"sp-rag-gateway/internal/authz"
	"sp-rag-gateway/internal/cache"
	"sp-rag-gateway/internal/config"
	"sp-rag-gateway/internal/handler"
	"sp-rag-gateway/internal/middleware"
	"sp-rag-gateway/internal/orchestrator"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// Local dev: load from project root; in Docker env vars come from compose
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load(".env")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// MinIO client
	minioClient, err := minio.New(cfg.MinIOEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""),
		Secure: cfg.MinIOUseSSL,
	})
	if err != nil {
		slog.Error("failed to create minio client", "error", err)
		os.Exit(1)
	}

	// Kafka writer
	kafkaWriter := &kafka.Writer{
		Addr:     kafka.TCP(cfg.KafkaBrokers...),
		Topic:    cfg.KafkaTopicUploaded,
		Balancer: &kafka.LeastBytes{},
	}

	// Qdrant client (gRPC)
	qdrantClient, err := qdrant.NewClient(&qdrant.Config{
		Host:   cfg.QdrantHost,
		Port:   cfg.QdrantGRPCPort,
		APIKey: cfg.QdrantAPIKey,
	})
	if err != nil {
		slog.Error("failed to create qdrant client", "error", err,
			"host", cfg.QdrantHost, "port", cfg.QdrantGRPCPort)
		os.Exit(1)
	}

	// OpenAI client
	openaiClient := openai.NewClient(cfg.OpenAIAPIKey)

	// Redis cache (exact + semantic)
	ttl := time.Duration(cfg.RedisCacheTTL) * time.Second
	redisCache, err := cache.NewRedisCache(
		cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB,
		ttl, cfg.RedisSimilarityThreshold, cfg.EmbeddingDimensions,
	)
	if err != nil {
		slog.Error("failed to create redis cache", "error", err, "addr", cfg.RedisAddr)
		os.Exit(1)
	}

	// Create RediSearch vector index for semantic cache
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := redisCache.EnsureIndex(ctx); err != nil {
		slog.Warn("failed to create semantic cache index (RediSearch may not be available)", "error", err)
	}
	cancel()

	// SpiceDB client (gRPC)
	authzClient, err := authz.NewAuthzClient(cfg.SpiceDBEndpoint, cfg.SpiceDBPresharedKey)
	if err != nil {
		slog.Error("failed to create spicedb client", "error", err,
			"endpoint", cfg.SpiceDBEndpoint)
		os.Exit(1)
	}

	// Query orchestrator (parallel pipeline)
	orch := orchestrator.New(cfg, openaiClient, authzClient, redisCache, qdrantClient)

	h := &handler.Handler{
		Config:       cfg,
		MinIO:        minioClient,
		KafkaWriter:  kafkaWriter,
		Authz:        authzClient,
		Orchestrator: orch,
	}

	app := fiber.New(fiber.Config{
		BodyLimit: 50 * 1024 * 1024, // 50MB
	})

	app.Use(recover.New())
	app.Use(middleware.CORS())
	app.Use(middleware.RequestLogger())

	api := app.Group("/api/v1")
	api.Get("/health", h.Health)
	api.Post("/documents/upload", h.Upload)
	api.Post("/query", h.Query)

	// Demo UI (static files)
	handler.RegisterStaticRoutes(app)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := app.Listen(":" + cfg.Port); err != nil {
			slog.Error("server error", "error", err)
		}
	}()

	slog.Info("gateway started", "port", cfg.Port)

	<-quit
	slog.Info("shutting down gateway...")

	if err := app.Shutdown(); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	if err := kafkaWriter.Close(); err != nil {
		slog.Error("kafka writer close error", "error", err)
	}
	if err := qdrantClient.Close(); err != nil {
		slog.Error("qdrant client close error", "error", err)
	}
	if err := redisCache.Close(); err != nil {
		slog.Error("redis cache close error", "error", err)
	}

	slog.Info("gateway stopped")
}
