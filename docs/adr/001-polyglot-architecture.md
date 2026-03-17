# ADR-001: Polyglot Architecture (Go + Python)

## Status
Accepted

## Context
Traditional RAG (Retrieval-Augmented Generation) systems are built as Python monoliths. While Python excels at AI/NLP tasks due to its ecosystem (OpenAI SDK, unstructured.io, etc.), it struggles with high-concurrency HTTP serving, efficient goroutine-style parallelism, and low-latency orchestration. For an enterprise-grade internal search engine that needs to handle concurrent users, the API layer becomes a bottleneck in a pure Python architecture.

Additionally, this project serves as the foundation for a master's thesis comparing monolithic Python RAG vs polyglot Go+Python RAG under load.

## Decision
Split the system into two domains:
- **Go API Gateway** — handles HTTP routing, authentication, caching, parallel orchestration (errgroup), and external service coordination (Qdrant, Redis, SpiceDB, OpenAI)
- **Python Worker** — handles AI/NLP processing: PDF text extraction (unstructured.io), chunking, embedding generation (OpenAI), and vector storage (Qdrant)

Communication between domains is asynchronous via Kafka (Redpanda).

## Alternatives Considered
- **Python monolith (FastAPI):** Simpler deployment, but limited concurrency. asyncio helps but doesn't match Go's goroutine model for CPU-bound orchestration. No basis for academic comparison.
- **Go monolith:** Go lacks mature NLP/PDF extraction libraries. Calling Python from Go via subprocess or gRPC adds complexity without clean separation.
- **Node.js API + Python worker:** Node has good async I/O but weaker type safety and less control over memory/concurrency compared to Go.

## Consequences
**Positive:**
- Go API achieves sub-millisecond routing and true parallel orchestration (embed + authz in parallel goroutines)
- Python worker uses the best-in-class NLP libraries without compromise
- Clean domain boundary via Kafka enables independent scaling and deployment
- Provides a real-world basis for academic performance comparison

**Negative:**
- Two languages increase onboarding complexity
- Deployment requires two Docker images and Kafka infrastructure
- Debugging cross-service issues requires distributed tracing (planned for Phase 6)
