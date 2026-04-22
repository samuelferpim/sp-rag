# ADR-015: Legal-Tech Vertical — Citation Verification

## Status
Accepted

## Context

The Legal-Tech vertical ("Guardião Tributário e Jurídico") targets law firms
and accounting offices querying DOU, Receita Federal normatives, and tax
legislation. In this domain, **hallucinating a law number is a
malpractice-grade failure**: an answer that confidently cites "Lei 9.999/2099"
when that law does not exist, or references an article number under the wrong
statute, can drive a lawyer to advise a client wrongly and create liability.

The existing self-reflection grounding check (ADR-013) verifies that claims
are supported by retrieved context in natural language — but it does not
cross-check specific legal identifiers against a structured source of truth.
A legally-coherent-sounding sentence with a fabricated law number can pass
the generic evaluator.

## Decision

Add a **citation verification** pass specific to the legal vertical, built on
infrastructure already in place:

1. The Python worker's smart chunking (ADR-011) already extracts entities —
   `articles`, `laws`, `decrees`, `cnpjs` — via regex and writes them into
   each Qdrant chunk's payload.
2. The orchestrator now surfaces those entities on `Source.Entities`
   (previously only `text/source_file/page` were pulled into the API
   response).
3. A new `internal/legal/citations.go` ports the same regex patterns to Go
   and applies them to the LLM's generated answer.
4. `VerifyCitations` returns the subset of cited references that do NOT
   appear in any retrieved chunk's entity set, matched per-category.
5. `legal.Service.HandleQuery` wraps the generic orchestrator and, after
   grounding succeeds, runs this verification. Behavior on unverified
   citations depends on the `LEGAL_STRICT_CITATION` flag (default true):
    - Strict: rewrite the answer with a fallback message; return
      `status: "unverified_citations"` plus the offending list for audit.
    - Lenient: keep the original answer and surface the warning list so the
      UI can flag it inline (useful for dev/demo).

The vertical mirrors Portaria's layout (ADR-014): a channel-agnostic
`Service` under `internal/legal/`, plus a single channel package
`internal/legal/web/` that exposes both the chat UI (`GET /legal`) and the
JSON endpoint (`POST /api/v1/legal/query`). Third-party integrations call
the JSON endpoint directly — the UI is a client of that contract, not a
second contract.

### Regex parity with the Python worker

`services/gateway/internal/legal/citations.go` and
`services/worker/app/etl.py` share the same regex shapes
(`_ARTICLE_RE`, `_LAW_RE`, `_DECREE_RE`, `_CNPJ_RE`). If they drift, the
verifier will reject legitimate citations or miss fabricated ones. Both
files carry a cross-referencing comment and are exercised by parity-style
tests in each repo half.

Post-processing (`normalize()` + trailing-punctuation trim) paper over
trivial differences between how the LLM writes a reference ("Lei nº
8.666/1993.") and how the worker stored it ("8.666/1993").

## Alternatives Considered

- **Let the evaluator do it.** The LLM-as-a-Judge prompt could ask "does
  every cited law exist in context?" — but the evaluator is a natural
  language check. Specific identifier verification is better handled by
  structured regex matching, which is deterministic, fast, and auditable.
- **Query a public law database (LexML) at runtime.** Authoritative but
  slow, introduces an external dependency in the hot path, and does not
  help when the firm has private/internal documents. Deferred to Phase 11
  (GraphRAG) as a complementary enrichment — not a replacement.
- **Reject any answer with citations not in retrieved entities, no
  exceptions.** Too blunt — a lawyer may genuinely paraphrase without a
  citation, or the LLM may reference a law by name rather than number.
  Strict mode only rejects when the verifier identifies a specific cited
  value that has no match, not when citations are absent.

## Consequences

**Positive:**
- Malpractice-grade failure mode (fabricated law numbers) now caught
  deterministically, independent of the LLM's self-reflection.
- Citation list in the response doubles as an audit trail — every
  response carries its verified/unverified citation breakdown.
- Same infrastructure supports future "Receita Federal", "DOU", and
  "Jurisprudência" tenants with zero code changes, just new documents.
- Dev-friendly: `LEGAL_STRICT_CITATION=false` lets the UI warn-not-block
  while iterating on prompts.

**Negative:**
- The verifier only catches citations matching our four regex families.
  Case law references (HC, RE, REsp, etc.) and jurisprudence-style cites
  are not verified yet — explicit follow-up.
- Regex drift risk between Python and Go: both sides need to be touched
  when the entity catalog expands.
- DOU ingestion pipeline is not yet automated — the vertical relies on
  manual uploads via the existing upload handler. `verticals/legal/ingest/`
  is scaffolded with a stub for the next iteration.

## References

- `internal/legal/service.go`, `internal/legal/citations.go`
- `internal/legal/web/handler.go`
- `services/worker/app/etl.py` (extract_entities)
- ADR-011 (Smart Chunking — source of the entity payload)
- ADR-013 (Self-Reflection Grounding — precedes citation verification)
- ADR-014 (Portaria Vertical — establishes the Service + channels layout)
