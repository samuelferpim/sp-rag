# ADR-005: Permission-Aware Semantic Cache

## Status
Accepted

## Context
Caching RAG responses dramatically reduces latency and LLM API costs. However, a naive cache (keyed only on query text or embedding) introduces a critical security vulnerability: User A queries "What are the financial results?", the response is cached, and User B (who lacks finance permissions) receives the same cached response.

We need a caching strategy that:
1. Provides cache hits for repeated queries by the same user (or users with identical permissions)
2. Never serves cached data to users with different permission sets
3. Supports both exact-match and semantic similarity lookups

## Decision
Implement a two-tier permission-aware cache using Redis Stack (RediSearch):

**Tier 1 — Exact Cache:**
- Key: `exact:{SHA-256(normalize(query) + "|" + permissionHash(sorted_permissions))}`
- Query normalization: lowercase, trim, collapse whitespace, strip punctuation
- Permission hash: SHA-256 of sorted, pipe-joined permission strings

**Tier 2 — Semantic Cache:**
- Uses RediSearch FLAT vector index with COSINE distance
- Each cached entry includes a TAG field with the permission hash
- KNN search is scoped to entries with matching permission hash: `@perm:{hash}=>[KNN 1 @vec $vec AS dist]`
- Similarity threshold: 0.92 (1 - cosine_distance)

Cache lookup order: semantic (broader match) → exact (fallback).

## Alternatives Considered
- **Naive cache (query-only key):** Simple but leaks data across permission boundaries. Unacceptable for enterprise use.
- **Per-user cache:** Safe but extremely low hit rate. Two users on the same team asking similar questions would never share cache entries.
- **Cache with permission validation on read:** Complex — would require storing the original permission set and re-checking on every cache hit. Adds latency and complexity.

## Consequences
**Positive:**
- Zero risk of cross-permission data leakage (proven in unit tests)
- Users with identical permissions benefit from shared cache
- Semantic cache catches paraphrased queries ("What is RAG?" ≈ "Explain retrieval augmented generation")
- Compliance-ready: auditable, deterministic cache isolation

**Negative:**
- Lower cache hit rate compared to naive caching (permission sets fragment the cache space)
- Every query requires an embedding call (~$0.0001/query) even for cache hits (embedding needed for semantic search)
- Redis Stack required (standard Redis lacks vector search capabilities)
- Additional complexity in cache key construction and index management
