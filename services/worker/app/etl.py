"""
ETL module — PDF extraction, cleaning, and smart chunking.

Pipeline:
PDF path → unstructured elements → filter noise → semantic chunking

Smart chunking preserves document structure: Title elements act as section
markers, content blocks (paragraphs) are kept whole when possible, and each
chunk carries metadata (section_title) for richer retrieval context.

Size limits use character count (not word count) for more predictable splits.
"""

import logging
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

from unstructured.documents.elements import (
    Element,
    Footer,
    Header,
    Image,
    PageBreak,
    Title,
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
    page: int              # 1-based page number of the first element in this chunk
    chunk_index: int       # sequential index within the document
    metadata: dict = field(default_factory=dict)  # e.g. {"section_title": "..."}


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
    """Split elements into semantically coherent text chunks.

    Groups content preserving section boundaries: Title elements act as
    section markers (stored in chunk metadata), and content blocks
    (paragraphs) are kept whole when possible. Section changes force a
    clean break (no overlap across sections); size-based splits carry
    trailing blocks as overlap for context continuity.

    Args:
        elements:         Filtered list of unstructured elements.
        chunk_size:       Target chunk size in characters.
        overlap:          Characters of overlap between consecutive chunks.
        min_chunk_length: Discard chunks shorter than this character count.

    Returns:
        Ordered list of TextChunk objects with section metadata.
    """
    if chunk_size <= overlap:
        raise ValueError(
            f"chunk_size ({chunk_size}) must be greater than overlap ({overlap})"
        )

    # ── Step 1: Convert elements to content blocks with section tracking ──
    current_section: str = ""
    blocks: list[tuple[str, int, str]] = []  # (text, page, section_title)

    for el in elements:
        text = str(el).strip()
        if not text:
            continue
        if isinstance(el, Title):
            current_section = text
            continue  # Title is a section marker, not content
        blocks.append((text, _get_page(el), current_section))

    if not blocks:
        logger.warning("No content blocks extracted from elements")
        return []

    # ── Step 2: Greedily group blocks into chunks ─────────────────────────
    chunks: list[TextChunk] = []
    chunk_idx = 0

    acc_texts: list[str] = []
    acc_pages: list[int] = []
    acc_section: str = blocks[0][2]
    acc_len: int = 0

    def _flush_chunk() -> None:
        """Emit accumulated blocks as a chunk if they meet min length."""
        nonlocal chunk_idx
        chunk_text = "\n\n".join(acc_texts)
        if len(chunk_text) >= min_chunk_length:
            chunks.append(
                TextChunk(
                    text=chunk_text,
                    page=acc_pages[0],
                    chunk_index=chunk_idx,
                    metadata={"section_title": acc_section},
                )
            )
            chunk_idx += 1

    def _overlap_carry() -> tuple[list[str], list[int], int]:
        """Return trailing blocks that fit within the overlap budget."""
        carry_t: list[str] = []
        carry_p: list[int] = []
        carry_len = 0
        for t, p in reversed(list(zip(acc_texts, acc_pages))):
            if carry_len + len(t) > overlap:
                break
            carry_t.insert(0, t)
            carry_p.insert(0, p)
            carry_len += len(t)
        return carry_t, carry_p, carry_len

    for text, page, section in blocks:
        # Section change → flush with clean break (no overlap)
        if section != acc_section and acc_texts:
            _flush_chunk()
            acc_texts, acc_pages, acc_len = [], [], 0
            acc_section = section

        # Size limit → flush with overlap for context continuity
        sep = 2 if acc_texts else 0  # "\n\n" separator
        if acc_len + sep + len(text) > chunk_size and acc_texts:
            _flush_chunk()
            acc_texts, acc_pages, acc_len = _overlap_carry()
            acc_section = section
            sep = 2 if acc_texts else 0

        acc_texts.append(text)
        acc_pages.append(page)
        acc_len += sep + len(text)

    # Flush remaining blocks
    if acc_texts:
        _flush_chunk()

    logger.info(
        "Chunking complete",
        extra={
            "block_count": len(blocks),
            "chunk_count": len(chunks),
            "chunk_size_chars": chunk_size,
            "overlap_chars": overlap,
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
