"""
Kafka consumer — document ingestion pipeline.

Event flow:
document.uploaded → download from MinIO → ETL → embed → Qdrant → publish document.processed (success) → publish document.failed (error)

Graceful shutdown is handled via SIGTERM / SIGINT: the consumer finishes
the current message before exiting, then commits offsets.
"""

import json
import logging
import signal
import tempfile
import threading
from datetime import datetime, timezone
from typing import Any

from confluent_kafka import Consumer, KafkaError, KafkaException, Producer
from minio import Minio
from openai import OpenAI
from qdrant_client import QdrantClient

from app.config import Config
from app.embedder import DocumentMetadata, build_openai_client, build_qdrant_client, process_document
from app.etl import process_pdf

logger = logging.getLogger(__name__)


# ── Shutdown flag ─────────────────────────────────────────────────────────────

_shutdown = threading.Event()


def _handle_signal(signum: int, frame: object) -> None:  # noqa: ARG001
    logger.info("Shutdown signal received", extra={"signal": signum})
    _shutdown.set()


signal.signal(signal.SIGTERM, _handle_signal)
signal.signal(signal.SIGINT, _handle_signal)


# ── Kafka helpers ─────────────────────────────────────────────────────────────


def build_consumer(config: Config) -> Consumer:
    """Create and subscribe a Kafka consumer.

    Args:
        config: Application configuration.

    Returns:
        Consumer subscribed to the uploaded-documents topic.
    """
    consumer = Consumer(
        {
            "bootstrap.servers": config.kafka_brokers,
            "group.id": config.kafka_group_id,
            "auto.offset.reset": config.kafka_auto_offset_reset,
            # Manual commit: we commit only after successful processing
            "enable.auto.commit": False,
        }
    )
    consumer.subscribe([config.kafka_topic_uploaded])
    logger.info(
        "Kafka consumer subscribed",
        extra={
            "brokers": config.kafka_brokers,
            "topic": config.kafka_topic_uploaded,
            "group": config.kafka_group_id,
        },
    )
    return consumer


def build_producer(config: Config) -> Producer:
    """Create a Kafka producer for result events.

    Args:
        config: Application configuration.

    Returns:
        Configured Kafka producer.
    """
    return Producer({"bootstrap.servers": config.kafka_brokers})


def build_minio_client(config: Config) -> Minio:
    """Create a MinIO client from configuration.

    Args:
        config: Application configuration.

    Returns:
        Configured Minio client.
    """
    return Minio(
        endpoint=config.minio_endpoint,
        access_key=config.minio_access_key,
        secret_key=config.minio_secret_key,
        secure=config.minio_secure,
    )


def _publish(producer: Producer, topic: str, payload: dict[str, Any]) -> None:
    """Serialize and produce a single JSON event.

    Args:
        producer: Kafka producer.
        topic:    Destination topic.
        payload:  Event payload (must be JSON-serializable).
    """
    producer.produce(
        topic,
        value=json.dumps(payload).encode("utf-8"),
        callback=lambda err, msg: logger.error(
            "Kafka delivery failed",
            extra={"topic": topic, "error": str(err)},
        )
        if err
        else None,
    )
    producer.poll(0)  # trigger callbacks without blocking


def _publish_processed(
    producer: Producer,
    config: Config,
    file_path: str,
    file_name: str,
    user_id: str,
    chunks_count: int,
) -> None:
    _publish(
        producer,
        config.kafka_topic_processed,
        {
            "file_path": file_path,
            "file_name": file_name,
            "user_id": user_id,
            "chunks_count": chunks_count,
            "processed_at": datetime.now(timezone.utc).isoformat(),
        },
    )


def _publish_failed(
    producer: Producer,
    config: Config,
    file_path: str,
    file_name: str,
    user_id: str,
    error: str,
) -> None:
    _publish(
        producer,
        config.kafka_topic_failed,
        {
            "file_path": file_path,
            "file_name": file_name,
            "user_id": user_id,
            "error": error,
            "failed_at": datetime.now(timezone.utc).isoformat(),
        },
    )


# ── Message processing ────────────────────────────────────────────────────────


def _parse_event(raw: bytes) -> dict[str, Any]:
    """Deserialize and validate a document.uploaded JSON event.

    Expected schema:
    {
        "file_path":   "documents/report.pdf",
        "file_name":   "report.pdf",
        "user_id":     "user_123",
        "permissions": ["finance_team"],
        "uploaded_at": "2026-03-10T10:00:00Z"
    }

    Args:
        raw: Raw message bytes from Kafka.

    Returns:
        Parsed event dictionary.

    Raises:
        ValueError: If required fields are missing or JSON is malformed.
    """
    try:
        event: dict[str, Any] = json.loads(raw.decode("utf-8"))
    except (json.JSONDecodeError, UnicodeDecodeError) as exc:
        raise ValueError(f"Invalid message encoding: {exc}") from exc

    required = {"file_path", "file_name", "user_id", "permissions", "uploaded_at"}
    missing = required - event.keys()
    if missing:
        raise ValueError(f"Missing required fields: {missing}")

    return event


def _process_event(
    event: dict[str, Any],
    config: Config,
    minio: Minio,
    qdrant: QdrantClient,
    openai_client: OpenAI,
) -> int:
    """Download, extract, embed, and store one document.

    Args:
        event:         Parsed document.uploaded event.
        config:        Application configuration.
        minio:         MinIO client.
        qdrant:        Qdrant client.
        openai_client: OpenAI client.

    Returns:
        Number of chunks ingested.

    Raises:
        Exception: Any error encountered during processing (caller handles).
    """
    file_path: str = event["file_path"]
    file_name: str = event["file_name"]

    logger.info("Processing document", extra={"file_path": file_path})

    # Download PDF from MinIO into a temporary file
    with tempfile.NamedTemporaryFile(suffix=".pdf", delete=True) as tmp:
        minio.fget_object(
            bucket_name=config.minio_bucket,
            object_name=file_path,
            file_path=tmp.name,
        )
        logger.debug("Downloaded from MinIO", extra={"file_path": file_path})
        chunks = process_pdf(tmp.name, config)

    if not chunks:
        raise ValueError(f"ETL produced zero chunks for {file_name}")

    metadata = DocumentMetadata(
        source_file=file_name,
        file_path=file_path,
        user_id=event["user_id"],
        permissions=event["permissions"],
        uploaded_at=event["uploaded_at"],
    )
    process_document(
        chunks=chunks,
        metadata=metadata,
        config=config,
        qdrant=qdrant,
        openai_client=openai_client,
    )
    return len(chunks)


# ── Main consumer loop ────────────────────────────────────────────────────────


def run(config: Config) -> None:
    """Start the consumer loop and block until a shutdown signal is received.

    Commits offsets manually after each successfully processed message.
    Publishes result events (processed / failed) for every message.

    Args:
        config: Application configuration.
    """
    consumer = build_consumer(config)
    producer = build_producer(config)
    minio = build_minio_client(config)
    qdrant = build_qdrant_client(config)
    openai_client = build_openai_client(config)

    logger.info("Worker started, waiting for messages")

    try:
        while not _shutdown.is_set():
            msg = consumer.poll(timeout=config.kafka_poll_timeout_s)

            if msg is None:
                continue

            if msg.error():
                if msg.error().code() == KafkaError._PARTITION_EOF:
                    # End of partition — not an error, just no new messages
                    continue
                raise KafkaException(msg.error())

            file_path = "<unknown>"
            file_name = "<unknown>"
            user_id = "<unknown>"

            try:
                event = _parse_event(msg.value())
                file_path = event["file_path"]
                file_name = event["file_name"]
                user_id = event["user_id"]

                chunks_count = _process_event(
                    event=event,
                    config=config,
                    minio=minio,
                    qdrant=qdrant,
                    openai_client=openai_client,
                )

                _publish_processed(
                    producer=producer,
                    config=config,
                    file_path=file_path,
                    file_name=file_name,
                    user_id=user_id,
                    chunks_count=chunks_count,
                )
                logger.info(
                    "Document ingested successfully",
                    extra={
                        "file_path": file_path,
                        "chunks": chunks_count,
                    },
                )

            except Exception as exc:
                logger.exception(
                    "Failed to process document",
                    extra={"file_path": file_path, "error": str(exc)},
                )
                _publish_failed(
                    producer=producer,
                    config=config,
                    file_path=file_path,
                    file_name=file_name,
                    user_id=user_id,
                    error=str(exc),
                )

            finally:
                # Always commit so we don't re-process on restart,
                # even if we published a failure event.
                consumer.commit(asynchronous=False)

    finally:
        logger.info("Flushing producer and closing consumer")
        producer.flush()
        consumer.close()
        logger.info("Worker stopped")
