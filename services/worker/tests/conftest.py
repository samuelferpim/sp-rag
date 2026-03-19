"""Shared fixtures for worker tests."""

from pathlib import Path
from unittest.mock import MagicMock

import pytest
from fpdf import FPDF


@pytest.fixture
def sample_pdf(tmp_path: Path) -> Path:
    """Generate a simple 2-page PDF for testing."""
    pdf = FPDF()

    pdf.add_page()
    pdf.set_font("Helvetica", size=12)
    pdf.multi_cell(0, 10, text=(
        "Chapter 1: Introduction to Distributed Systems\n\n"
        "A distributed system is a collection of independent computers that appears "
        "to its users as a single coherent system. The key characteristics include "
        "concurrency, lack of a global clock, and independent failures. "
        "Modern distributed systems must handle network partitions gracefully "
        "while maintaining consistency and availability trade-offs as described "
        "by the CAP theorem. This document explores the fundamental principles "
        "and practical patterns for building reliable distributed applications."
    ))

    pdf.add_page()
    pdf.multi_cell(0, 10, text=(
        "Chapter 2: Consensus Protocols\n\n"
        "Consensus protocols are the backbone of distributed systems. "
        "Raft and Paxos are the most well-known protocols. "
        "Raft was designed to be more understandable than Paxos while providing "
        "the same guarantees. In Raft, a leader is elected among the nodes, "
        "and the leader is responsible for log replication. "
        "If the leader fails, a new election is triggered. "
        "This ensures that the system can continue to operate even when "
        "individual nodes fail, maintaining strong consistency guarantees."
    ))

    pdf_path = tmp_path / "test_document.pdf"
    pdf.output(str(pdf_path))
    return pdf_path


@pytest.fixture
def corrupted_file(tmp_path: Path) -> Path:
    """Create a file with invalid content (not a PDF)."""
    path = tmp_path / "corrupted.pdf"
    path.write_bytes(b"this is not a pdf file at all")
    return path


@pytest.fixture
def text_file(tmp_path: Path) -> Path:
    """Create a plain text file with .txt extension."""
    path = tmp_path / "readme.txt"
    path.write_text("This is a plain text file, not a PDF.")
    return path


@pytest.fixture
def mock_config():
    """Create a mock Config with default values (character-based chunking)."""
    cfg = MagicMock()
    cfg.chunk_size = 2000
    cfg.chunk_overlap = 200
    cfg.min_chunk_length = 50
    cfg.openai_embedding_model = "text-embedding-3-small"
    cfg.openai_embedding_dimensions = 1536
    cfg.openai_batch_size = 100
    cfg.qdrant_collection = "test_collection"
    return cfg
