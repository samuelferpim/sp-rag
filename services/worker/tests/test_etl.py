"""Tests for the ETL module (text extraction + chunking)."""

from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from app.etl import TextChunk, chunk_elements, filter_elements, process_pdf


class TestFilterElements:
    """Test element filtering (headers, footers, empty elements)."""

    def test_removes_headers(self):
        header = MagicMock()
        header.category = "Header"
        header.text = "Page Header"
        body = MagicMock()
        body.category = "NarrativeText"
        body.text = "Body content"

        result = filter_elements([header, body])
        assert len(result) == 1
        assert result[0].text == "Body content"

    def test_removes_footers(self):
        footer = MagicMock()
        footer.category = "Footer"
        footer.text = "Page 1"
        body = MagicMock()
        body.category = "NarrativeText"
        body.text = "Body content"

        result = filter_elements([footer, body])
        assert len(result) == 1

    def test_removes_page_breaks(self):
        pb = MagicMock()
        pb.category = "PageBreak"
        pb.text = ""
        body = MagicMock()
        body.category = "NarrativeText"
        body.text = "Content"

        result = filter_elements([pb, body])
        assert len(result) == 1

    def test_removes_empty_elements(self):
        empty = MagicMock()
        empty.category = "NarrativeText"
        empty.text = "   "
        body = MagicMock()
        body.category = "NarrativeText"
        body.text = "Content"

        result = filter_elements([empty, body])
        assert len(result) == 1

    def test_removes_image_elements(self):
        img = MagicMock()
        img.category = "Image"
        img.text = ""
        body = MagicMock()
        body.category = "NarrativeText"
        body.text = "Content"

        result = filter_elements([img, body])
        assert len(result) == 1


class TestChunkElements:
    """Test sliding-window chunking."""

    def _make_element(self, text: str, page: int = 1) -> MagicMock:
        elem = MagicMock()
        elem.text = text
        elem.metadata = MagicMock()
        elem.metadata.page_number = page
        return elem

    def test_basic_chunking(self, mock_config):
        mock_config.chunk_size = 10
        mock_config.chunk_overlap = 2
        mock_config.min_chunk_length = 3
        words = " ".join([f"word{i}" for i in range(25)])
        elem = self._make_element(words)

        chunks = chunk_elements([elem], mock_config)
        assert len(chunks) > 1
        assert all(isinstance(c, TextChunk) for c in chunks)

    def test_chunk_size_respected(self, mock_config):
        mock_config.chunk_size = 20
        mock_config.chunk_overlap = 5
        mock_config.min_chunk_length = 5
        words = " ".join([f"word{i}" for i in range(100)])
        elem = self._make_element(words)

        chunks = chunk_elements([elem], mock_config)
        for chunk in chunks:
            word_count = len(chunk.text.split())
            assert word_count <= mock_config.chunk_size + 5  # small tolerance

    def test_overlap_between_chunks(self, mock_config):
        mock_config.chunk_size = 10
        mock_config.chunk_overlap = 3
        mock_config.min_chunk_length = 3
        words = " ".join([f"w{i}" for i in range(30)])
        elem = self._make_element(words)

        chunks = chunk_elements([elem], mock_config)
        if len(chunks) >= 2:
            words_0 = set(chunks[0].text.split())
            words_1 = set(chunks[1].text.split())
            overlap = words_0 & words_1
            assert len(overlap) > 0, "Consecutive chunks should have overlapping words"

    def test_preserves_page_numbers(self, mock_config):
        mock_config.chunk_size = 20
        mock_config.chunk_overlap = 5
        mock_config.min_chunk_length = 5
        elem1 = self._make_element("Page one content here " * 5, page=1)
        elem2 = self._make_element("Page two content here " * 5, page=2)

        chunks = chunk_elements([elem1, elem2], mock_config)
        pages = {c.page for c in chunks}
        assert 1 in pages or 2 in pages

    def test_filters_short_chunks(self, mock_config):
        mock_config.chunk_size = 50
        mock_config.chunk_overlap = 10
        mock_config.min_chunk_length = 20
        elem = self._make_element("short text")

        chunks = chunk_elements([elem], mock_config)
        # Short text below min_chunk_length may produce 0 or 1 chunks
        for chunk in chunks:
            assert len(chunk.text.split()) >= mock_config.min_chunk_length

    def test_chunk_index_sequential(self, mock_config):
        mock_config.chunk_size = 10
        mock_config.chunk_overlap = 2
        mock_config.min_chunk_length = 3
        words = " ".join([f"word{i}" for i in range(50)])
        elem = self._make_element(words)

        chunks = chunk_elements([elem], mock_config)
        for i, chunk in enumerate(chunks):
            assert chunk.chunk_index == i


class TestProcessPdf:
    """Test full PDF processing pipeline."""

    def test_process_real_pdf(self, sample_pdf, mock_config):
        chunks = process_pdf(str(sample_pdf), mock_config)
        assert len(chunks) > 0
        assert all(isinstance(c, TextChunk) for c in chunks)
        assert all(c.text.strip() for c in chunks)

    def test_invalid_file_raises(self, corrupted_file, mock_config):
        with pytest.raises(Exception):
            process_pdf(str(corrupted_file), mock_config)

    def test_nonexistent_file_raises(self, mock_config):
        with pytest.raises(Exception):
            process_pdf("/nonexistent/path.pdf", mock_config)
