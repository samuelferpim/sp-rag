"""
ETL module — PDF extraction, cleaning, and chunking.

Pipeline:
PDF path → unstructured elements → filter noise → sliding-window chunks

Chunking uses word count as a token approximation (no tiktoken dependency).
One word ≈ one token is conservative but avoids an extra dependency.
"""

import logging
import tempfile
from dataclasses import dataclass
from pathlib import Path

from unstructured.documents.elements import (
    Element,
    Footer,
    Header,
    Image,
    PageBreak,
)
from unstructured.partition.pdf import partition_pdf

from app.config import Config

logger = logging.getLogger(__name__)

# Element types treated as noise (structural artefacts, not content)
_NOISE_TYPES = (Header, Footer, PageBreak, Image)


@dataclass(frozen=True)
class TextChunk:
    """A single text chunk ready for embedding."""

    text: str
    page: int          # 1-based page number of the first word in this chunk
    chunk_index: int   # sequential index within the document


def extract_elements(pdf_path: str) -> list[Element]:
    """Extract all elements from a PDF using unstructured.

    Args:
        pdf_path: Absolute path to the PDF file.

    Returns:
        List of unstructured Element objects with text and metadata.

    Raises:
        FileNotFoundError: If pdf_path does not exist.
        Exception: If unstructured cannot parse the file.
    """
    path = Path(pdf_path)
    if not path.exists():
        raise FileNotFoundError(f"PDF not found: {pdf_path}")

    logger.info("Extracting elements from PDF", extra={"path": pdf_path})
    elements = partition_pdf(
        filename=str(path),
        strategy="fast",          # OCR not required for text-based PDFs
        include_page_breaks=True, # lets us track page boundaries
    )
    logger.info(
        "Extraction complete",
        extra={"path": pdf_path, "element_count": len(elements)},
    )
    return elements  # type: ignore[return-value]


def filter_elements(elements: list[Element]) -> list[Element]:
    """Remove headers, footers, page breaks, images, and empty elements.

    Args:
        elements: Raw list of unstructured elements.

    Returns:
        Filtered list retaining only content-bearing elements.
    """
    filtered: list[Element] = []
    for el in elements:
        if isinstance(el, _NOISE_TYPES):
            continue
        text = str(el).strip()
        if not text:
            continue
        filtered.append(el)

    logger.debug(
        "Filtered elements",
        extra={"before": len(elements), "after": len(filtered)},
    )
    return filtered


def _get_page(element: Element) -> int:
    """Extract 1-based page number from element metadata, defaulting to 1."""
    try:
        page = element.metadata.page_number  # type: ignore[attr-defined]
        return int(page) if page is not None else 1
    except AttributeError:
        return 1


def chunk_elements(
    elements: list[Element],
    chunk_size: int,
    overlap: int,
    min_chunk_length: int,
) -> list[TextChunk]:
    """Split a list of content elements into overlapping text chunks.

    Uses a sliding window over individual words, preserving page provenance
    for each chunk (page of the first word in the window).

    Args:
        elements:         Filtered list of unstructured elements.
        chunk_size:       Target chunk size in words (≈ tokens).
        overlap:          Number of words shared between consecutive chunks.
        min_chunk_length: Discard chunks shorter than this word count.

    Returns:
        Ordered list of TextChunk objects.
    """
    if chunk_size <= overlap:
        raise ValueError(
            f"chunk_size ({chunk_size}) must be greater than overlap ({overlap})"
        )

    # Flatten all elements into a (word, page) stream
    words: list[tuple[str, int]] = []
    for el in elements:
        page = _get_page(el)
        for word in str(el).split():
            words.append((word, page))

    if not words:
        logger.warning("No words extracted from elements")
        return []

    chunks: list[TextChunk] = []
    step = chunk_size - overlap
    chunk_index = 0
    i = 0

    while i < len(words):
        window = words[i : i + chunk_size]
        text = " ".join(w for w, _ in window)

        if len(window) >= min_chunk_length:
            chunks.append(
                TextChunk(
                    text=text,
                    page=window[0][1],
                    chunk_index=chunk_index,
                )
            )
            chunk_index += 1

        i += step

    logger.info(
        "Chunking complete",
        extra={
            "word_count": len(words),
            "chunk_count": len(chunks),
            "chunk_size": chunk_size,
            "overlap": overlap,
        },
    )
    return chunks


def process_pdf(pdf_path: str, config: Config) -> list[TextChunk]:
    """Full ETL pipeline: PDF → clean text chunks.

    This is the primary entry point for the ETL module.

    Args:
        pdf_path: Absolute path to the PDF file on disk.
        config:   Application configuration.

    Returns:
        List of TextChunk objects ready for embedding.

    Raises:
        FileNotFoundError: If the PDF file does not exist.
        ValueError:        If chunking parameters are invalid.
    """
    elements = extract_elements(pdf_path)
    elements = filter_elements(elements)

    if not elements:
        logger.warning(
            "No content elements found after filtering",
            extra={"path": pdf_path},
        )
        return []

    chunks = chunk_elements(
        elements=elements,
        chunk_size=config.chunk_size,
        overlap=config.chunk_overlap,
        min_chunk_length=config.min_chunk_length,
    )
    return chunks


def process_pdf_bytes(data: bytes, config: Config) -> list[TextChunk]:
    """Run the ETL pipeline on raw PDF bytes (e.g. downloaded from MinIO).

    Writes bytes to a named temp file, processes, then cleans up.

    Args:
        data:   Raw PDF bytes.
        config: Application configuration.

    Returns:
        List of TextChunk objects.
    """
    with tempfile.NamedTemporaryFile(suffix=".pdf", delete=True) as tmp:
        tmp.write(data)
        tmp.flush()
        return process_pdf(tmp.name, config)
