"""
Embedder module — OpenAI embeddings → Qdrant vector store.

Responsibilities:
    1. Ensure the Qdrant collection exists with correct settings.
    2. Batch-embed text chunks via OpenAI text-embedding-3-small.
    3. Upsert vectors + metadata payload into Qdrant.
"""

import logging
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone

from openai import OpenAI
from qdrant_client import QdrantClient
from qdrant_client.http import models as qdrant_models

from app.config import Config
from app.etl import TextChunk

logger = logging.getLogger(__name__)


@dataclass
class DocumentMetadata:
    """Metadata attached to every vector in Qdrant for a given document.

    tenant_id is mandatory: the gateway enforces a Qdrant Must-filter on this key
    for every query, guaranteeing absolute isolation between clients.
    """

    tenant_id: str            # owning tenant (e.g. "acme-corp")
    source_file: str          # original file name (e.g. "relatorio_2026.pdf")
    file_path: str            # full MinIO path (e.g. "documents/acme-corp/relatorio_2026.pdf")
    user_id: str              # uploader user ID
    permissions: list[str]    # permission tags (e.g. ["finance_team"])
    uploaded_at: str          # ISO-8601 timestamp from the Kafka event


def build_qdrant_client(config: Config) -> QdrantClient:
    """Create and return a Qdrant client from configuration.

    Args:
        config: Application configuration.

    Returns:
        Configured QdrantClient instance.
    """
    return QdrantClient(
        host=config.qdrant_host,
        port=config.qdrant_port,
        api_key=config.qdrant_api_key,
        https=False,
    )


def build_openai_client(config: Config) -> OpenAI:
    """Create and return an OpenAI client from configuration.

    Args:
        config: Application configuration.

    Returns:
        Configured OpenAI client instance.
    """
    return OpenAI(api_key=config.openai_api_key)


def ensure_collection(
    qdrant: QdrantClient,
    collection_name: str,
    vector_size: int,
) -> None:
    """Create the Qdrant collection (and its tenant payload index) if absent.

    Uses Cosine distance, which is standard for sentence embeddings. Also creates
    a keyword payload index on ``tenant_id`` so the mandatory tenant Must-filter
    stays cheap as the collection grows.

    Args:
        qdrant:          Qdrant client.
        collection_name: Name of the collection to create.
        vector_size:     Dimensionality of the embedding vectors.
    """
    existing = {c.name for c in qdrant.get_collections().collections}
    if collection_name not in existing:
        logger.info(
            "Creating Qdrant collection",
            extra={"collection": collection_name, "vector_size": vector_size},
        )
        qdrant.create_collection(
            collection_name=collection_name,
            vectors_config=qdrant_models.VectorParams(
                size=vector_size,
                distance=qdrant_models.Distance.COSINE,
            ),
        )
        logger.info("Collection created", extra={"collection": collection_name})
    else:
        logger.debug(
            "Collection already exists", extra={"collection": collection_name}
        )

    # Payload index on tenant_id — keeps the tenant Must-filter O(log n)
    # instead of a full payload scan. Idempotent: Qdrant ignores duplicates.
    try:
        qdrant.create_payload_index(
            collection_name=collection_name,
            field_name="tenant_id",
            field_schema=qdrant_models.PayloadSchemaType.KEYWORD,
        )
    except Exception as exc:  # noqa: BLE001 — Qdrant raises different types across versions
        # Already-exists errors are expected; log others as warnings.
        logger.debug(
            "tenant_id payload index not created (may already exist)",
            extra={"error": str(exc)},
        )


def embed_chunks(
    chunks: list[TextChunk],
    openai_client: OpenAI,
    model: str,
    batch_size: int,
) -> list[list[float]]:
    """Generate embeddings for a list of text chunks via OpenAI.

    Sends requests in batches to respect API limits.

    Args:
        chunks:        List of text chunks to embed.
        openai_client: Configured OpenAI client.
        model:         Embedding model name.
        batch_size:    Max number of texts per API request.

    Returns:
        List of embedding vectors, one per chunk, in the same order.

    Raises:
        openai.APIError: On API communication failures.
    """
    texts = [chunk.text for chunk in chunks]
    all_embeddings: list[list[float]] = []

    for start in range(0, len(texts), batch_size):
        batch = texts[start : start + batch_size]
        logger.debug(
            "Embedding batch",
            extra={"start": start, "end": start + len(batch), "model": model},
        )
        response = openai_client.embeddings.create(input=batch, model=model)
        # Response items are ordered by index, guaranteed by the API
        batch_embeddings = [item.embedding for item in response.data]
        all_embeddings.extend(batch_embeddings)

    logger.info(
        "Embedding complete",
        extra={"chunk_count": len(chunks), "model": model},
    )
    return all_embeddings


def upsert_vectors(
    qdrant: QdrantClient,
    collection_name: str,
    chunks: list[TextChunk],
    embeddings: list[list[float]],
    metadata: DocumentMetadata,
) -> None:
    """Upsert chunk vectors and their metadata payload into Qdrant.

    Each point gets a deterministic UUID derived from file_path + chunk_index
    so re-processing the same document is idempotent.

    Args:
        qdrant:          Qdrant client.
        collection_name: Target collection name.
        chunks:          Text chunks (must be same length as embeddings).
        embeddings:      Embedding vectors for each chunk.
        metadata:        Document-level metadata attached to every point.

    Raises:
        ValueError: If chunks and embeddings lengths differ.
    """
    if len(chunks) != len(embeddings):
        raise ValueError(
            f"chunks ({len(chunks)}) and embeddings ({len(embeddings)}) "
            "must have the same length"
        )

    now = datetime.now(timezone.utc).isoformat()
    points: list[qdrant_models.PointStruct] = []

    for chunk, vector in zip(chunks, embeddings):
        # Deterministic ID: avoids duplicate vectors on re-ingestion
        point_id = str(
            uuid.uuid5(
                uuid.NAMESPACE_URL,
                f"{metadata.file_path}::{chunk.chunk_index}",
            )
        )
        payload = {
            "text": chunk.text,
            "tenant_id": metadata.tenant_id,
            "source_file": metadata.source_file,
            "file_path": metadata.file_path,
            "page": chunk.page,
            "chunk_index": chunk.chunk_index,
            "section_title": chunk.metadata.get("section_title", ""),
            "permissions": metadata.permissions,
            "uploaded_by": metadata.user_id,
            "uploaded_at": metadata.uploaded_at,
            "created_at": now,
        }
        # Legal-tech / GraphRAG seed metadata: articles, laws, decrees, CNPJs.
        # Only attached when the chunk text actually matched an entity regex.
        if entities := chunk.metadata.get("entities"):
            payload["entities"] = entities
        points.append(
            qdrant_models.PointStruct(
                id=point_id,
                vector=vector,
                payload=payload,
            )
        )

    qdrant.upsert(collection_name=collection_name, points=points)
    logger.info(
        "Vectors upserted",
        extra={
            "collection": collection_name,
            "point_count": len(points),
            "source_file": metadata.source_file,
        },
    )


def process_document(
    chunks: list[TextChunk],
    metadata: DocumentMetadata,
    config: Config,
    qdrant: QdrantClient,
    openai_client: OpenAI,
) -> None:
    """End-to-end: embed chunks and store in Qdrant.

    This is the primary entry point for the embedder module.

    Args:
        chunks:        Pre-processed text chunks from the ETL module.
        metadata:      Document provenance and access-control metadata.
        config:        Application configuration.
        qdrant:        Shared Qdrant client (caller manages lifecycle).
        openai_client: Shared OpenAI client (caller manages lifecycle).

    Raises:
        ValueError:      If chunks is empty.
        openai.APIError: On embedding API failure.
    """
    if not chunks:
        raise ValueError("Cannot process a document with zero chunks")

    ensure_collection(
        qdrant=qdrant,
        collection_name=config.qdrant_collection,
        vector_size=config.openai_embedding_dimensions,
    )

    embeddings = embed_chunks(
        chunks=chunks,
        openai_client=openai_client,
        model=config.openai_embedding_model,
        batch_size=config.openai_batch_size,
    )

    upsert_vectors(
        qdrant=qdrant,
        collection_name=config.qdrant_collection,
        chunks=chunks,
        embeddings=embeddings,
        metadata=metadata,
    )
