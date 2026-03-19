# ADR-011: Smart Chunking (Section-Aware, Character-Based)

## Status
Accepted

## Context
The original chunking strategy used a naive sliding window over word count (~512 words per chunk). This approach broke content mid-sentence, ignored document structure (titles, sections), and produced chunks without semantic context about where they came from in the document.

For a RAG system, chunk quality directly impacts retrieval quality: semantically incoherent chunks lead to poor embeddings and irrelevant search results.

## Decision
Replace the word-based sliding window with **smart/semantic chunking** that:

1. **Recognizes document structure:** `Title` elements from `unstructured.io` act as section markers. Each chunk carries a `section_title` metadata field identifying which section it belongs to.
2. **Preserves semantic coherence:** Content blocks (paragraphs) are kept whole when possible. Section boundaries force clean chunk breaks (no overlap across sections).
3. **Uses character-based limits** (~2000 chars, ~500 tokens) instead of word count for more predictable splits.
4. **Carries overlap** only on size-based splits within a section, providing context continuity without mixing sections.

## Alternatives Considered
- **LangChain/LlamaIndex chunking:** Rejected per project convention (no RAG frameworks; we build the pipeline from scratch).
- **Sentence-level splitting:** Too granular; many sentences lack standalone meaning without their paragraph context.
- **Fixed token count with tiktoken:** Adds a dependency; character-based is a close enough approximation (~4 chars/token).

## Consequences
**Positive:**
- Chunks are semantically coherent and carry section context for richer retrieval
- `section_title` in Qdrant payload enables future section-level filtering
- Character-based limits are more predictable than word-based

**Negative:**
- Existing vectors in Qdrant from the old chunking strategy are incompatible (re-ingestion needed)
- Single blocks larger than `chunk_size` are emitted as-is (no mid-paragraph splitting)
