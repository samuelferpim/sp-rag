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
	"sp-rag-gateway/internal/legal"
	legalweb "sp-rag-gateway/internal/legal/web"
	"sp-rag-gateway/internal/middleware"
	"sp-rag-gateway/internal/orchestrator"
	"sp-rag-gateway/internal/portaria"
	portariaweb "sp-rag-gateway/internal/portaria/web"
	"sp-rag-gateway/internal/portaria/whatsapp"
	"sp-rag-gateway/internal/rag"
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

	// Semantic router (query complexity classification)
	router := rag.NewSemanticRouter(openaiClient, cfg.OpenAIFastModel)

	// LLM-as-a-Judge evaluator (grounding check)
	evaluator := rag.NewLLMEvaluator(openaiClient, cfg.OpenAIFastModel)

	// Query orchestrator (parallel pipeline)
	orch := orchestrator.New(cfg, openaiClient, authzClient, redisCache, qdrantClient, router, evaluator)

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
	app.Use(middleware.TenantResolver())

	api := app.Group("/api/v1")
	api.Get("/health", h.Health)
	api.Post("/documents/upload", h.Upload)
	api.Post("/query", h.Query)

	// Demo UI (static files)
	handler.RegisterStaticRoutes(app)

	// Portaria vertical (optional). Enable with PORTARIA_ENABLED=true.
	if cfg.PortariaEnabled {
		portariaSvc := portaria.NewService(orch, cfg.PortariaTopK, cfg.PortariaHumanContact)
		portariaweb.NewHandler(portariaSvc).Register(app, "/api/v1")

		wppCfg := whatsapp.Config{
			VerifyToken:     cfg.PortariaWhatsAppVerifyTok,
			AppSecret:       cfg.PortariaWhatsAppAppSecret,
			AccessToken:     cfg.PortariaWhatsAppAccessTok,
			TenantByPhoneID: cfg.PortariaWhatsAppPhoneMap,
		}
		if wppCfg.AppSecret != "" && wppCfg.AccessToken != "" && len(wppCfg.TenantByPhoneID) > 0 {
			sender := whatsapp.NewMetaClient(wppCfg.AccessToken)
			whatsapp.NewHandler(wppCfg, portariaSvc, sender).Register(app)
			slog.Info("portaria: whatsapp channel enabled",
				"tenants", len(wppCfg.TenantByPhoneID))
		} else {
			slog.Warn("portaria: whatsapp channel disabled (missing APP_SECRET, ACCESS_TOKEN, or PHONE_MAP)")
		}
		slog.Info("portaria: web channel enabled at /portaria and /api/v1/portaria/chat")
	}

	// Legal vertical (optional). Enable with LEGAL_ENABLED=true.
	if cfg.LegalEnabled {
		legalSvc := legal.NewService(orch, cfg.LegalTopK, cfg.LegalStrictCitation)
		legalweb.NewHandler(legalSvc).Register(app, "/api/v1")
		slog.Info("legal: enabled at /legal and /api/v1/legal/query",
			"strict_citation", cfg.LegalStrictCitation,
			"top_k", cfg.LegalTopK,
		)
	}

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
