package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port                     string
	MinIOEndpoint            string
	MinIOAccessKey           string
	MinIOSecretKey           string
	MinIOBucket              string
	MinIOUseSSL              bool
	KafkaBrokers             []string
	KafkaTopicUploaded       string
	KafkaTopicProcessed      string
	KafkaTopicFailed         string
	QdrantHost               string
	QdrantGRPCPort           int
	QdrantAPIKey             string
	QdrantCollection         string
	OpenAIAPIKey             string
	OpenAIEmbeddingModel     string
	OpenAIChatModel          string
	OpenAIFastModel          string
	EmbeddingDimensions      int
	QueryTopK                int
	RedisAddr                string
	RedisPassword            string
	RedisDB                  int
	RedisCacheTTL            int
	RedisSimilarityThreshold float64
	SpiceDBEndpoint          string
	SpiceDBPresharedKey      string
	QueryTimeoutSeconds      int
}

func Load() (*Config, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	return &Config{
		Port:                     envOr("GATEWAY_PORT", "8081"),
		MinIOEndpoint:            envOr("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey:           envOr("MINIO_ROOT_USER", "minioadmin"),
		MinIOSecretKey:           envOr("MINIO_ROOT_PASSWORD", "minioadmin"),
		MinIOBucket:              envOr("MINIO_BUCKET", "documents"),
		MinIOUseSSL:              envOrBool("MINIO_USE_SSL", false),
		KafkaBrokers:             strings.Split(envOr("KAFKA_BROKER", "localhost:9092"), ","),
		KafkaTopicUploaded:       envOr("KAFKA_TOPIC_UPLOADED", "document.uploaded"),
		KafkaTopicProcessed:      envOr("KAFKA_TOPIC_PROCESSED", "document.processed"),
		KafkaTopicFailed:         envOr("KAFKA_TOPIC_FAILED", "document.failed"),
		QdrantHost:               envOr("QDRANT_HOST", "localhost"),
		QdrantGRPCPort:           envOrInt("QDRANT_GRPC_PORT", 6334),
		QdrantAPIKey:             os.Getenv("QDRANT_API_KEY"),
		QdrantCollection:         envOr("QDRANT_COLLECTION", "sp_rag_docs"),
		OpenAIAPIKey:             apiKey,
		OpenAIEmbeddingModel:     envOr("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"),
		OpenAIChatModel:          envOr("OPENAI_CHAT_MODEL", "gpt-4o-mini"),
		OpenAIFastModel:          envOr("OPENAI_FAST_MODEL", "gpt-4o-mini"),
		EmbeddingDimensions:      envOrInt("OPENAI_EMBEDDING_DIMENSIONS", 1536),
		QueryTopK:                envOrInt("QUERY_TOP_K", 5),
		RedisAddr:                envOr("REDIS_ADDR", "localhost:6379"),
		RedisPassword:            os.Getenv("REDIS_PASSWORD"),
		RedisDB:                  envOrInt("REDIS_DB", 0),
		RedisCacheTTL:            envOrInt("REDIS_CACHE_TTL_SECONDS", 3600),
		RedisSimilarityThreshold: envOrFloat("REDIS_SIMILARITY_THRESHOLD", 0.92),
		SpiceDBEndpoint:          envOr("SPICEDB_ENDPOINT", "localhost:50051"),
		SpiceDBPresharedKey:      envOr("SPICEDB_PRESHARED_KEY", "sprag_dev_key"),
		QueryTimeoutSeconds:      envOrInt("QUERY_TIMEOUT_SECONDS", 30),
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envOrFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func envOrBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
