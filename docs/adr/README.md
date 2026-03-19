# Architecture Decision Records (ADRs)

This directory contains the Architecture Decision Records for the SP-RAG project.

ADRs document significant architectural decisions made during the design and implementation of the system. Each ADR describes the context, the decision, alternatives considered, and the consequences (both positive and negative).

## Index

| # | Decision | Status |
|---|----------|--------|
| [001](001-polyglot-architecture.md) | Polyglot Architecture (Go + Python) | Accepted |
| [002](002-redpanda-over-kafka.md) | Redpanda over Apache Kafka | Accepted |
| [003](003-qdrant-over-alternatives.md) | Qdrant over Alternative Vector DBs | Accepted |
| [004](004-spicedb-zero-trust.md) | SpiceDB for Zero-Trust Access Control | Accepted |
| [005](005-permission-aware-cache.md) | Permission-Aware Semantic Cache | Accepted |
| [006](006-embedding-before-cache.md) | Embedding Before Cache Lookup | Accepted |
| [007](007-fiber-over-gin.md) | Fiber over Gin/Chi | Accepted |
| [008](008-project-structure.md) | Monorepo with Domain Separation | Accepted |
| [009](009-minio-for-storage.md) | MinIO for Object Storage | Accepted |
| [010](010-confluent-kafka-over-kafka-python.md) | confluent-kafka over kafka-python | Accepted |
| [011](011-smart-chunking.md) | Smart Chunking (Section-Aware, Character-Based) | Accepted |
| [012](012-semantic-router.md) | Semantic Router (Query Complexity Classification) | Accepted |
| [013](013-self-reflection-grounding.md) | Self-Reflection / LLM-as-a-Judge for Grounding | Accepted |

## Format

Each ADR follows this template:

```markdown
# ADR-XXX: Title

## Status
Accepted | Superseded | Deprecated

## Context
Why this decision needed to be made.

## Decision
What we decided.

## Alternatives Considered
What we rejected and why.

## Consequences
Positive and negative impacts.
```
