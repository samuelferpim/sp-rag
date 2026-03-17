# ADR-003: Qdrant over Alternative Vector Databases

## Status
Accepted

## Context
The system needs a vector database to store document embeddings and perform similarity search. Critical requirements:
1. Payload filtering at query time (to enforce permission-based access control)
2. SDKs for both Go and Python
3. Self-hosted option (no vendor lock-in)
4. Production-ready with good performance characteristics

## Decision
Use Qdrant as the vector database.

Qdrant is written in Rust, provides both REST (port 6333) and gRPC (port 6334) APIs, and has official SDKs for Go and Python. Its payload filtering capability allows embedding permission metadata directly into vector points and filtering at query time.

## Alternatives Considered
- **Pinecone:** Managed SaaS only — vendor lock-in, no self-hosting, data leaves our infrastructure. Not suitable for enterprise deployments with data residency requirements.
- **Milvus:** Self-hosted and feature-rich, but significantly more complex to operate (requires etcd, MinIO, multiple components). The Go SDK is less mature than Qdrant's.
- **Weaviate:** Good feature set, but the Go client was less mature at evaluation time. GraphQL-first API adds unnecessary complexity for our simple query patterns.
- **pgvector (PostgreSQL):** Simple to set up, but lacks advanced filtering capabilities and scales poorly for high-dimensional vectors with payload-based filtering.

## Consequences
**Positive:**
- Payload filtering enables permission-aware vector search at the database level (defense-in-depth with SpiceDB)
- Excellent Go SDK (`go.qdrant.io/client`) with gRPC support for high-performance queries
- Self-hosted via Docker, no vendor lock-in
- Written in Rust — efficient memory usage and consistent latency
- Single binary deployment, simple to operate

**Negative:**
- Smaller community compared to Pinecone or Milvus
- No built-in replication in single-node mode (sufficient for this project)
- Requires separate infrastructure component vs embedding in PostgreSQL
