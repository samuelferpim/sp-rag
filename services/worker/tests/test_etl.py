"""Tests for the ETL module (text extraction + smart chunking)."""

from pathlib import Path
from unittest.mock import MagicMock

import pytest

from unstructured.documents.elements import (
    Footer,
    Header,
    Image,
    NarrativeText,
    PageBreak,
    Title,
)

from app.etl import TextChunk, chunk_elements, filter_elements, process_pdf


class TestFilterElements:
    """Test element filtering (headers, footers, empty elements)."""

    def test_removes_headers(self):
        header = MagicMock(spec=Header)
        header.text = "Page Header"
        body = MagicMock(spec=NarrativeText)
        body.text = "Body content"
        body.__str__ = lambda self: self.text

        result = filter_elements([header, body])
        assert len(result) == 1

    def test_removes_footers(self):
        footer = MagicMock(spec=Footer)
        footer.text = "Page 1"
        body = MagicMock(spec=NarrativeText)
        body.text = "Body content"
        body.__str__ = lambda self: self.text

        result = filter_elements([footer, body])
        assert len(result) == 1

    def test_removes_page_breaks(self):
        pb = MagicMock(spec=PageBreak)
        pb.text = ""
        body = MagicMock(spec=NarrativeText)
        body.text = "Content"
        body.__str__ = lambda self: self.text

        result = filter_elements([pb, body])
        assert len(result) == 1

    def test_removes_empty_elements(self):
        empty = MagicMock(spec=NarrativeText)
        empty.__str__ = lambda self: "   "
        body = MagicMock(spec=NarrativeText)
        body.text = "Content"
        body.__str__ = lambda self: self.text

        result = filter_elements([empty, body])
        assert len(result) == 1

    def test_removes_image_elements(self):
        img = MagicMock(spec=Image)
        img.text = ""
        body = MagicMock(spec=NarrativeText)
        body.text = "Content"
        body.__str__ = lambda self: self.text

        result = filter_elements([img, body])
        assert len(result) == 1


class TestChunkElements:
    """Test smart/semantic chunking with section awareness."""

    def _make_element(self, text: str, page: int = 1) -> MagicMock:
        """Create a mock NarrativeText element."""
        elem = MagicMock(spec=NarrativeText)
        elem.text = text
        elem.__str__ = lambda self: self.text
        elem.metadata = MagicMock()
        elem.metadata.page_number = page
        return elem

    def _make_title(self, text: str, page: int = 1) -> MagicMock:
        """Create a mock Title element (section marker)."""
        elem = MagicMock(spec=Title)
        elem.text = text
        elem.__str__ = lambda self: self.text
        elem.metadata = MagicMock()
        elem.metadata.page_number = page
        return elem

    def test_basic_chunking(self):
        """Content below chunk_size produces a single chunk."""
        elem = self._make_element("A short paragraph of content.", page=1)

        chunks = chunk_elements([elem], chunk_size=200, overlap=50, min_chunk_length=10)
        assert len(chunks) == 1
        assert isinstance(chunks[0], TextChunk)
        assert chunks[0].text == "A short paragraph of content."

    def test_chunk_size_splits(self):
        """Content exceeding chunk_size is split into multiple chunks."""
        paragraphs = [
            self._make_element(f"Paragraph {i} " + "x" * 100, page=1)
            for i in range(10)
        ]

        chunks = chunk_elements(
            paragraphs, chunk_size=300, overlap=50, min_chunk_length=20
        )
        assert len(chunks) > 1
        assert all(isinstance(c, TextChunk) for c in chunks)

    def test_keeps_whole_elements_together(self):
        """Whole paragraphs should not be split mid-text."""
        elem1 = self._make_element("First paragraph with some content here.")
        elem2 = self._make_element("Second paragraph with different content.")

        chunks = chunk_elements(
            [elem1, elem2], chunk_size=5000, overlap=100, min_chunk_length=10
        )
        # Both fit in one chunk
        assert len(chunks) == 1
        assert "First paragraph" in chunks[0].text
        assert "Second paragraph" in chunks[0].text
        # Paragraphs joined with double newline
        assert "\n\n" in chunks[0].text

    def test_section_title_in_metadata(self):
        """Title elements populate section_title in chunk metadata."""
        title = self._make_title("Introduction")
        body = self._make_element("This is the intro content for the document.")

        chunks = chunk_elements(
            [title, body], chunk_size=5000, overlap=100, min_chunk_length=10
        )
        assert len(chunks) == 1
        assert chunks[0].metadata["section_title"] == "Introduction"

    def test_section_change_forces_new_chunk(self):
        """A new Title element forces a clean chunk boundary."""
        title1 = self._make_title("Section A")
        body1 = self._make_element("Content for section A is here and long enough.")
        title2 = self._make_title("Section B")
        body2 = self._make_element("Content for section B is here and long enough.")

        chunks = chunk_elements(
            [title1, body1, title2, body2],
            chunk_size=5000, overlap=100, min_chunk_length=10,
        )
        assert len(chunks) == 2
        assert chunks[0].metadata["section_title"] == "Section A"
        assert chunks[1].metadata["section_title"] == "Section B"
        assert "section A" in chunks[0].text
        assert "section B" in chunks[1].text

    def test_overlap_between_chunks(self):
        """Size-based splits carry trailing content as overlap."""
        # Create blocks that will cause a size-based split
        elems = [
            self._make_element(f"Block number {i}. " + "y" * 80)
            for i in range(10)
        ]

        chunks = chunk_elements(
            elems, chunk_size=300, overlap=100, min_chunk_length=20
        )
        if len(chunks) >= 2:
            # Overlap means some text appears in both consecutive chunks
            words_0 = set(chunks[0].text.split())
            words_1 = set(chunks[1].text.split())
            shared = words_0 & words_1
            assert len(shared) > 0, "Consecutive chunks should share overlap text"

    def test_preserves_page_numbers(self):
        """Page number comes from the first element in each chunk."""
        elem1 = self._make_element("Page one content here. " * 5, page=1)
        elem2 = self._make_element("Page two content here. " * 5, page=2)

        chunks = chunk_elements(
            [elem1, elem2], chunk_size=5000, overlap=100, min_chunk_length=10
        )
        assert chunks[0].page == 1

    def test_filters_short_chunks(self):
        """Chunks below min_chunk_length are discarded."""
        elem = self._make_element("tiny")

        chunks = chunk_elements(
            [elem], chunk_size=5000, overlap=100, min_chunk_length=100
        )
        assert len(chunks) == 0

    def test_chunk_index_sequential(self):
        """Chunk indices are sequential starting from 0."""
        elems = [
            self._make_element(f"Paragraph {i}. " + "z" * 100)
            for i in range(10)
        ]

        chunks = chunk_elements(
            elems, chunk_size=300, overlap=50, min_chunk_length=20
        )
        for i, chunk in enumerate(chunks):
            assert chunk.chunk_index == i

    def test_no_section_title_when_no_titles(self):
        """Without Title elements, section_title defaults to empty string."""
        elem = self._make_element("Content without any title element preceding it.")

        chunks = chunk_elements(
            [elem], chunk_size=5000, overlap=100, min_chunk_length=10
        )
        assert chunks[0].metadata["section_title"] == ""

    def test_invalid_params_raises(self):
        """chunk_size must be greater than overlap."""
        elem = self._make_element("Some content.")
        with pytest.raises(ValueError):
            chunk_elements([elem], chunk_size=50, overlap=100, min_chunk_length=10)

    def test_empty_elements_returns_empty(self):
        """No content blocks produces an empty list."""
        title = self._make_title("Just a title, no body")

        chunks = chunk_elements(
            [title], chunk_size=5000, overlap=100, min_chunk_length=10
        )
        assert chunks == []

    def test_multiple_sections_with_size_splits(self):
        """Complex scenario: multiple sections, some requiring size-based splits."""
        elements = [
            self._make_title("Chapter 1"),
            self._make_element("Intro paragraph for chapter one. " * 10),
            self._make_element("More details about chapter one topic. " * 10),
            self._make_title("Chapter 2"),
            self._make_element("Chapter two begins with this content. " * 10),
        ]

        chunks = chunk_elements(
            elements, chunk_size=200, overlap=50, min_chunk_length=20
        )
        assert len(chunks) >= 3
        # First chunks belong to Chapter 1
        assert chunks[0].metadata["section_title"] == "Chapter 1"
        # Last chunk(s) belong to Chapter 2
        assert chunks[-1].metadata["section_title"] == "Chapter 2"


class TestProcessPdf:
    """Test full PDF processing pipeline."""

    def test_process_real_pdf(self, sample_pdf, mock_config):
        chunks = process_pdf(str(sample_pdf), mock_config)
        assert len(chunks) > 0
        assert all(isinstance(c, TextChunk) for c in chunks)
        assert all(c.text.strip() for c in chunks)
        # Every chunk should have metadata dict with section_title
        assert all("section_title" in c.metadata for c in chunks)

    def test_invalid_file_returns_empty(self, corrupted_file, mock_config):
        """Corrupted files are handled gracefully, returning empty list."""
        result = process_pdf(str(corrupted_file), mock_config)
        assert result == []

    def test_nonexistent_file_raises(self, mock_config):
        with pytest.raises(FileNotFoundError):
            process_pdf("/nonexistent/path.pdf", mock_config)
