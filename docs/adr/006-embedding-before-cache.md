# ADR-006: Embedding Before Cache Lookup

## Status
Accepted

## Context
The semantic cache requires the query's embedding vector to perform a KNN similarity search. This creates a chicken-and-egg problem: should we check the cache before or after generating the embedding?

Option A: Embed first, then check cache (both semantic and exact).
Option B: Check exact cache first (no embedding needed), then embed only on exact miss for semantic cache lookup.

## Decision
Always generate the embedding before any cache lookup (Option A).

The embedding is generated in parallel with the SpiceDB permission resolution (via errgroup), so it does not add sequential latency. By the time the parallel phase completes, both the embedding vector and the user's teams are available for immediate cache lookup.

## Alternatives Considered
- **Exact cache first, embed on miss (Option B):** Saves the embedding cost on exact cache hits, but adds sequential latency. The exact cache hit rate is low in practice (queries must match exactly after normalization), so the savings are minimal while the architectural complexity increases. It also prevents the parallel execution of embedding + authz.

## Consequences
**Positive:**
- Enables parallel execution: embed and authz goroutines run simultaneously
- Simpler code path: embedding is always available for all downstream operations
- Semantic cache (higher hit rate) is checked first, maximizing cache effectiveness
- Consistent latency profile: no conditional branching in the hot path

**Negative:**
- Every query incurs an embedding API call (~$0.0001 per query with text-embedding-3-small)
- On exact cache hits, the embedding is generated but unused for the semantic cache
- Slight increase in OpenAI API usage (negligible at expected query volumes)
