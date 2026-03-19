"""
Configuration — all settings sourced from environment variables.

Loaded once at startup via load_config(). No magic numbers in other modules.
"""

import os
from dataclasses import dataclass, field


def _env_int(key: str, default: int) -> int:
    return int(os.getenv(key, str(default)))


def _env_bool(key: str, default: bool) -> bool:
    return os.getenv(key, str(default)).lower() in ("true", "1", "yes")


@dataclass(frozen=True)
class Config:
    # ── Kafka / Redpanda ──────────────────────────────────────────────
    kafka_brokers: str = field(
        default_factory=lambda: os.getenv("KAFKA_BROKERS", "localhost:9092")
    )
    kafka_group_id: str = field(
        default_factory=lambda: os.getenv("KAFKA_GROUP_ID", "sp-rag-worker")
    )
    kafka_topic_uploaded: str = field(
        default_factory=lambda: os.getenv("KAFKA_TOPIC_UPLOADED", "document.uploaded")
    )
    kafka_topic_processed: str = field(
        default_factory=lambda: os.getenv("KAFKA_TOPIC_PROCESSED", "document.processed")
    )
    kafka_topic_failed: str = field(
        default_factory=lambda: os.getenv("KAFKA_TOPIC_FAILED", "document.failed")
    )
    kafka_auto_offset_reset: str = field(
        default_factory=lambda: os.getenv("KAFKA_AUTO_OFFSET_RESET", "earliest")
    )
    kafka_poll_timeout_s: float = field(
        default_factory=lambda: float(os.getenv("KAFKA_POLL_TIMEOUT_S", "1.0"))
    )

    # ── MinIO ─────────────────────────────────────────────────────────
    minio_endpoint: str = field(
        default_factory=lambda: os.getenv("MINIO_ENDPOINT", "localhost:9000")
    )
    minio_access_key: str = field(
        default_factory=lambda: os.getenv("MINIO_ROOT_USER", "sprag")
    )
    minio_secret_key: str = field(
        default_factory=lambda: os.getenv("MINIO_ROOT_PASSWORD", "sprag12345")
    )
    minio_bucket: str = field(
        default_factory=lambda: os.getenv("MINIO_BUCKET", "documents")
    )
    minio_secure: bool = field(
        default_factory=lambda: _env_bool("MINIO_SECURE", False)
    )

    # ── OpenAI ────────────────────────────────────────────────────────
    openai_api_key: str = field(
        default_factory=lambda: os.getenv("OPENAI_API_KEY", "")
    )
    openai_embedding_model: str = field(
        default_factory=lambda: os.getenv(
            "OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"
        )
    )
    # text-embedding-3-small produces 1536-dimensional vectors
    openai_embedding_dimensions: int = field(
        default_factory=lambda: _env_int("OPENAI_EMBEDDING_DIMENSIONS", 1536)
    )
    # Max chunks per OpenAI batch request (API limit: 2048 inputs)
    openai_batch_size: int = field(
        default_factory=lambda: _env_int("OPENAI_BATCH_SIZE", 100)
    )

    # ── Qdrant ────────────────────────────────────────────────────────
    qdrant_host: str = field(
        default_factory=lambda: os.getenv("QDRANT_HOST", "localhost")
    )
    qdrant_port: int = field(
        default_factory=lambda: _env_int("QDRANT_REST_PORT", 6333)
    )
    qdrant_api_key: str | None = field(
        default_factory=lambda: os.getenv("QDRANT_API_KEY") or None
    )
    qdrant_collection: str = field(
        default_factory=lambda: os.getenv("QDRANT_COLLECTION", "documents")
    )

    # ── ETL / Chunking ────────────────────────────────────────────────
    # Smart chunking uses character count (~4 chars ≈ 1 token)
    chunk_size: int = field(
        default_factory=lambda: _env_int("CHUNK_SIZE", 2000)
    )
    chunk_overlap: int = field(
        default_factory=lambda: _env_int("CHUNK_OVERLAP", 200)
    )
    # Chunks shorter than this (in characters) are discarded
    min_chunk_length: int = field(
        default_factory=lambda: _env_int("MIN_CHUNK_LENGTH", 50)
    )


def load_config() -> Config:
    """Load configuration from environment variables.

    Call once at startup. Import and pass the returned Config object
    everywhere — never call os.getenv() outside this module.
    """
    cfg = Config()
    if not cfg.openai_api_key:
        raise ValueError("OPENAI_API_KEY is required but not set")
    return cfg
