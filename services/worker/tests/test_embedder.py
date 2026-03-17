"""Tests for the embedder module (OpenAI embeddings → Qdrant)."""

from unittest.mock import MagicMock

import pytest

from app.embedder import DocumentMetadata, embed_chunks, upsert_vectors
from app.etl import TextChunk


@pytest.fixture
def sample_chunks() -> list[TextChunk]:
    return [
        TextChunk(text="Distributed systems overview", page=1, chunk_index=0),
        TextChunk(text="Consensus protocols like Raft", page=2, chunk_index=1),
        TextChunk(text="CAP theorem implications", page=2, chunk_index=2),
    ]


@pytest.fixture
def sample_metadata() -> DocumentMetadata:
    return DocumentMetadata(
        source_file="report.pdf",
        file_path="documents/report.pdf",
        user_id="user_123",
        permissions=["engineering_team"],
        uploaded_at="2026-03-10T10:00:00Z",
    )


class TestEmbedChunks:
    """Test embedding generation with mocked OpenAI."""

    def test_generates_embeddings(self, sample_chunks, mock_config):
        mock_client = MagicMock()
        mock_response = MagicMock()
        mock_response.data = [
            MagicMock(embedding=[0.1] * 1536),
            MagicMock(embedding=[0.2] * 1536),
            MagicMock(embedding=[0.3] * 1536),
        ]
        mock_client.embeddings.create.return_value = mock_response

        vectors = embed_chunks(
            sample_chunks, mock_client, mock_config.openai_embedding_model, mock_config.openai_batch_size
        )

        assert len(vectors) == 3
        assert len(vectors[0]) == 1536
        mock_client.embeddings.create.assert_called_once()

    def test_preserves_chunk_order(self, sample_chunks, mock_config):
        mock_client = MagicMock()
        mock_response = MagicMock()
        mock_response.data = [
            MagicMock(embedding=[float(i)] * 1536)
            for i in range(len(sample_chunks))
        ]
        mock_client.embeddings.create.return_value = mock_response

        vectors = embed_chunks(
            sample_chunks, mock_client, mock_config.openai_embedding_model, mock_config.openai_batch_size
        )

        assert vectors[0][0] == 0.0
        assert vectors[1][0] == 1.0
        assert vectors[2][0] == 2.0

    def test_handles_api_error(self, sample_chunks, mock_config):
        mock_client = MagicMock()
        mock_client.embeddings.create.side_effect = Exception("API rate limit")

        with pytest.raises(Exception, match="rate limit"):
            embed_chunks(
                sample_chunks, mock_client, mock_config.openai_embedding_model, mock_config.openai_batch_size
            )

    def test_batching_large_input(self, mock_config):
        mock_config.openai_batch_size = 2
        chunks = [
            TextChunk(text=f"Chunk {i}", page=1, chunk_index=i)
            for i in range(5)
        ]

        mock_client = MagicMock()
        mock_response = MagicMock()
        mock_response.data = [MagicMock(embedding=[0.1] * 1536)] * 2
        mock_response2 = MagicMock()
        mock_response2.data = [MagicMock(embedding=[0.1] * 1536)] * 2
        mock_response3 = MagicMock()
        mock_response3.data = [MagicMock(embedding=[0.1] * 1536)] * 1
        mock_client.embeddings.create.side_effect = [
            mock_response, mock_response2, mock_response3
        ]

        vectors = embed_chunks(
            chunks, mock_client, mock_config.openai_embedding_model, mock_config.openai_batch_size
        )
        assert len(vectors) == 5
        assert mock_client.embeddings.create.call_count == 3


class TestDocumentMetadata:
    """Test metadata construction."""

    def test_metadata_fields(self, sample_metadata):
        assert sample_metadata.source_file == "report.pdf"
        assert sample_metadata.file_path == "documents/report.pdf"
        assert sample_metadata.user_id == "user_123"
        assert sample_metadata.permissions == ["engineering_team"]
        assert sample_metadata.uploaded_at == "2026-03-10T10:00:00Z"

    def test_metadata_permissions_list(self):
        meta = DocumentMetadata(
            source_file="doc.pdf",
            file_path="documents/doc.pdf",
            user_id="alice",
            permissions=["finance_team", "admin"],
            uploaded_at="2026-01-01T00:00:00Z",
        )
        assert len(meta.permissions) == 2
        assert "finance_team" in meta.permissions


class TestUpsertVectors:
    """Test Qdrant upsert with mocked client."""

    def test_upsert_creates_points(self, sample_chunks, sample_metadata, mock_config):
        mock_qdrant = MagicMock()
        vectors = [[0.1] * 1536 for _ in sample_chunks]

        upsert_vectors(
            mock_qdrant, mock_config.qdrant_collection, sample_chunks, vectors, sample_metadata
        )

        mock_qdrant.upsert.assert_called_once()
        call_args = mock_qdrant.upsert.call_args
        assert call_args.kwargs["collection_name"] == "test_collection"
        points = call_args.kwargs["points"]
        assert len(points) == 3

    def test_upsert_includes_metadata(self, sample_chunks, sample_metadata, mock_config):
        mock_qdrant = MagicMock()
        vectors = [[0.1] * 1536 for _ in sample_chunks]

        upsert_vectors(
            mock_qdrant, mock_config.qdrant_collection, sample_chunks, vectors, sample_metadata
        )

        points = mock_qdrant.upsert.call_args.kwargs["points"]
        payload = points[0].payload
        assert payload["source_file"] == "report.pdf"
        assert payload["permissions"] == ["engineering_team"]
        assert payload["uploaded_by"] == "user_123"
