# ADR-012: Semantic Router (Query Complexity Classification)

## Status
Accepted

## Context
All queries were routed to the same LLM model regardless of complexity. Simple factual queries ("What port does Qdrant use?") consumed the same resources and latency as complex analytical queries ("Explain the authentication flow end-to-end"). This wastes API budget and adds unnecessary latency for straightforward lookups.

## Decision
Implement a **Semantic Router** pattern that classifies each query before the LLM generation step:

1. A `Router` interface in `internal/rag/router.go` with a `Classify(ctx, query) (Complexity, error)` method.
2. `SemanticRouter` implementation calls the LLM with an optimized classification prompt that returns `{"complexity": "simples" | "complexa"}`.
3. The router runs **in parallel** with embedding and authz (Phase 1 goroutine), adding zero latency to the critical path.
4. Simple queries use `OPENAI_FAST_MODEL` (default: `gpt-4o-mini`); complex queries use `OPENAI_CHAT_MODEL`.
5. On any error (API failure, bad JSON, unknown value), the router **defaults to complex** (safer to use the better model).

## Alternatives Considered
- **Keyword/regex-based routing:** Fragile, hard to maintain, poor accuracy on edge cases.
- **Embedding similarity to archetypes:** Requires maintaining a set of archetype queries; less flexible than LLM classification.
- **Client-side classification:** Shifts complexity to frontend; doesn't work for API consumers.

## Consequences
**Positive:**
- Reduces API costs by routing simple queries to cheaper models
- Zero added latency (classification runs in parallel with existing Phase 1 work)
- Fail-safe: defaults to the best model on any error

**Negative:**
- Adds one extra LLM call per query (though it's cheap: ~50 tokens, fast model)
- Classification accuracy depends on prompt quality; edge cases may be misclassified
