# SP-RAG (Secure Polyglot RAG)

![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?style=for-the-badge&logo=go)
![Python](https://img.shields.io/badge/Python-3.12+-3776AB?style=for-the-badge&logo=python)
![Kafka](https://img.shields.io/badge/Redpanda_(Kafka)-231F20?style=for-the-badge&logo=apache-kafka)
![Qdrant](https://img.shields.io/badge/Qdrant-Vector_DB-FF5252?style=for-the-badge)
![SpiceDB](https://img.shields.io/badge/SpiceDB-Zero_Trust-4285F4?style=for-the-badge)

An enterprise-grade **Retrieval-Augmented Generation** system with document-level access control, semantic caching, and a polyglot architecture (Go + Python).

SP-RAG tackles three critical problems in production AI systems: **LLM latency** (semantic cache cuts repeat queries from ~6s to ~400ms), **API costs** (cached responses skip OpenAI entirely), and **data governance** (SpiceDB ensures the LLM only sees what the user is allowed to see).

This project is also the foundation for a master's thesis comparing monolithic vs polyglot RAG architectures under load.

---

## Architecture

```
                          ┌──────────────┐
                          │   Browser    │
                          │   Demo UI    │
                          └──────┬───────┘
                                 │
                                 v
┌────────────────────────────────────────────────────────────┐
│                  Go API Gateway (Fiber)                     │
│                                                            │
│  Phase 1 (parallel)          Phase 2        Phase 3        │
│  ┌──────────┐ ┌────────┐   ┌───────┐   ┌──────┐ ┌─────┐  │
│  │ Embed    │ │ AuthZ  │   │ Cache │   │Qdrant│ │ LLM │  │
│  │ (OpenAI) │ │(SpiceDB│   │(Redis)│   │Search│ │(GPT)│  │
│  └────┬─────┘ └───┬────┘   └───┬───┘   └──┬───┘ └──┬──┘  │
│       │           │            │           │        │      │
└───────┼───────────┼────────────┼───────────┼────────┼──────┘
        v           v            v           v        v
   ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐ ┌──────┐
   │ OpenAI │  │SpiceDB │  │ Redis  │  │ Qdrant │ │OpenAI│
   │Embed   │  │        │  │ Stack  │  │        │ │Chat  │
   └────────┘  └────────┘  └────────┘  └────────┘ └──────┘
                                            ^
┌───────────────────────────────────────────┼────────────────┐
│              Python Worker                │                │
│                                           │                │
│  Kafka Consumer -> ETL -> Embed -> Upsert─┘                │
│  (unstructured)  (chunk)  (OpenAI)                         │
└────────────────────────────────────────────────────────────┘
        ^                                        ^
        │          ┌──────────────┐              │
        └──────────│  Redpanda    │──────────────┘
                   │  (Kafka)     │
                   └──────────────┘
                          ^
                   ┌──────────────┐
                   │    MinIO     │
                   │  (S3)       │
                   └──────────────┘
```

**Query pipeline:**
1. **Phase 1 (parallel):** Embed query via OpenAI + resolve user teams via SpiceDB (goroutines + errgroup)
2. **Phase 2:** Check semantic cache (Redis) -> if hit, return immediately (~400ms)
3. **Phase 3:** Qdrant vector search (with permission filter) -> SpiceDB post-filter -> LLM -> cache result -> return (~6s)

---

## Tech Stack

| Layer | Technology | Why |
|-------|-----------|-----|
| API Gateway | **Go** (Fiber) | Low-latency orchestration, goroutines for parallelism |
| AI Worker | **Python** | `unstructured.io` for PDF extraction, OpenAI SDK |
| Message Broker | **Redpanda** | Kafka-compatible, no JVM/Zookeeper, ~512MB RAM |
| Vector DB | **Qdrant** | Payload filtering for permissions, excellent Go SDK |
| Auth | **SpiceDB** | Google Zanzibar model, gRPC-native, fail-closed |
| Cache | **Redis Stack** | RediSearch for vector similarity + exact hash cache |
| Storage | **MinIO** | S3-compatible, works same in dev and prod |
| LLM | **OpenAI** | GPT-4o-mini (chat), text-embedding-3-small (embeddings) |

---

## Quickstart

### Prerequisites

- **Docker** (with Docker Compose) or **Colima**
- **Go 1.25+**
- **Python 3.12+**
- An **OpenAI API key**

### 1. Setup

```bash
git clone https://github.com/samuelferpim/sp-rag.git
cd sp-rag

# Create .env from template
cp .env.example .env
# Edit .env and add your OpenAI API key
```

> **Important:** If you have a local Redis running on port 6379, keep `REDIS_PORT=6380` in `.env` to avoid conflicts (the Redis Stack container needs the RediSearch module).

### 2. Start Infrastructure

```bash
make up          # Start all containers (Qdrant, Redis, Redpanda, MinIO, SpiceDB)
make topics      # Create Kafka topics
make spicedb-seed  # Apply permission schema + seed test users
```

### 3. Run the Services

Open **two terminals:**

```bash
# Terminal 1 — Go API Gateway
make gateway
```

```bash
# Terminal 2 — Python Worker
make worker
```

### 4. Verify Everything

```bash
# Health check
curl http://localhost:8081/api/v1/health
# {"service":"sp-rag-gateway","status":"ok"}
```

Open the **Demo UI** at http://localhost:8081

---

## Usage Guide

### Ingest a Document

```bash
# Seed a test PDF (Bitcoin whitepaper) into MinIO + publish Kafka event
make seed
```

The Python worker will automatically:
1. Download the PDF from MinIO
2. Extract text with `unstructured.io`
3. Chunk into ~512-token overlapping windows
4. Generate embeddings via OpenAI
5. Store vectors + metadata in Qdrant

Watch it happen: `make logs-worker`

### Query with Access Control

The seed script creates these SpiceDB relationships:

| User | Teams | Can view |
|------|-------|----------|
| `alice` | finance_team, hr_team | relatorio_financeiro, hr_policy |
| `bob` | eng_team | engineering_roadmap |
| `charlie` | finance_team, eng_team | relatorio_financeiro, engineering_roadmap |
| `samuca` | engineering_team, admin | sample_pdf (seeded doc) |

```bash
# Authorized query (samuca has engineering_team permission)
curl -s -X POST http://localhost:8081/api/v1/query \
  -H "Content-Type: application/json" \
  -d '{"query": "What is Bitcoin?", "user_id": "samuca"}' | jq .

# Unauthorized query (alice does NOT have engineering_team)
curl -s -X POST http://localhost:8081/api/v1/query \
  -H "Content-Type: application/json" \
  -d '{"query": "What is Bitcoin?", "user_id": "alice"}' | jq .
# Returns: "No relevant documents found for your query."
```

### Semantic Cache in Action

Run the same query twice:

```bash
# First call: ~6s (embeds query + Qdrant search + LLM generation)
curl -s -X POST http://localhost:8081/api/v1/query \
  -H "Content-Type: application/json" \
  -d '{"query": "Explain the Bitcoin whitepaper", "user_id": "samuca"}' | jq '.timing'

# Second call: ~400ms (semantic cache hit, skips Qdrant + LLM)
curl -s -X POST http://localhost:8081/api/v1/query \
  -H "Content-Type: application/json" \
  -d '{"query": "What does the Bitcoin paper say?", "user_id": "samuca"}' | jq '.cached, .timing'
# cached: true — even though the wording is different!
```

### Upload Your Own Document

```bash
curl -X POST http://localhost:8081/api/v1/documents/upload \
  -F "file=@/path/to/your/document.pdf" \
  -F "user_id=samuca" \
  -F "permissions=engineering_team,admin"
# Returns 202 Accepted — worker processes asynchronously
```

### Demo UI

Open http://localhost:8081 for an interactive web interface where you can:
- Upload documents
- Run queries as different users
- See response timing and sources
- Monitor service health status

---

## API Reference

### `GET /api/v1/health`
Returns gateway status.

### `POST /api/v1/documents/upload`
Upload a PDF for ingestion.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file` | multipart file | yes | PDF file |
| `user_id` | string | yes | Uploader user ID |
| `permissions` | string | yes | Comma-separated team names |

**Response:** `202 Accepted`

### `POST /api/v1/query`
Query the RAG pipeline.

```json
{
  "query": "What is Bitcoin?",
  "user_id": "samuca"
}
```

**Response:**
```json
{
  "answer": "Bitcoin is a peer-to-peer electronic cash system...",
  "sources": [
    {"file_name": "sample.pdf", "page": 1, "score": 0.42, "snippet": "..."}
  ],
  "model": "gpt-4o-mini",
  "cached": false,
  "timing": {
    "embed_ms": 528,
    "authz_ms": 68,
    "cache_ms": 4,
    "qdrant_ms": 6,
    "llm_ms": 5253,
    "total_ms": 5799
  }
}
```

---

## Service URLs

| Service | URL |
|---------|-----|
| **Demo UI** | http://localhost:8081 |
| **API** | http://localhost:8081/api/v1 |
| Qdrant Dashboard | http://localhost:6333/dashboard |
| Redpanda Console | http://localhost:8080 |
| MinIO Console | http://localhost:9001 (user: `sprag` / pass: `sprag12345`) |
| SpiceDB HTTP | http://localhost:8443 |

---

## Makefile Commands

```
make setup           # First-time: .env + infra + topics + health check
make up              # Start all infrastructure
make down            # Stop all infrastructure
make restart         # Restart all
make health          # Run health check script
make status          # Show container status
make logs            # Tail all logs
make logs-<svc>      # Tail specific service (e.g. make logs-qdrant)
make clean           # Stop + delete all volumes (destructive)

make topics          # Create Kafka topics
make topics-list     # List Kafka topics

make gateway         # Run Go API locally
make worker          # Run Python worker locally
make worker-deps     # Install Python deps

make spicedb-schema  # Write permission schema
make spicedb-seed    # Seed test users + teams + documents
make seed            # Upload sample PDF + publish Kafka event

make fmt             # Format Go + Python
make lint            # Lint Go + Python
make test            # Run all tests (Go + Python)
make test-go         # Run Go tests only
make test-python     # Run Python tests only
make test-e2e        # Run E2E tests (requires services running)
```

---

## Project Structure

```
sp-rag/
├── docker-compose.yml
├── Makefile
├── .env.example
├── services/
│   ├── gateway/                # Go API Gateway
│   │   ├── cmd/main.go
│   │   ├── Dockerfile
│   │   ├── web/index.html      # Demo UI (SPA)
│   │   └── internal/
│   │       ├── handler/        # HTTP handlers + static routes
│   │       ├── middleware/     # CORS, request logging
│   │       ├── orchestrator/  # Query pipeline (parallel phases)
│   │       ├── rag/            # Prompt building, LLM calls
│   │       ├── cache/          # Redis exact + semantic cache
│   │       ├── authz/          # SpiceDB integration
│   │       └── config/         # Environment config loader
│   └── worker/                 # Python Worker
│       ├── app/
│       │   ├── __main__.py     # Entry point
│       │   ├── consumer.py     # Kafka consumer loop
│       │   ├── etl.py          # PDF extraction + chunking
│       │   ├── embedder.py     # OpenAI embeddings -> Qdrant
│       │   └── config.py       # Configuration
│       ├── tests/              # pytest suite
│       ├── Dockerfile
│       └── requirements.txt
├── infra/spicedb/schema.zed    # Permission model (Zanzibar)
├── scripts/
│   ├── check-infra.sh          # Health check
│   ├── seed_data.sh            # Upload test PDF
│   ├── seed_spicedb.sh         # Seed SpiceDB permissions
│   └── e2e_test.sh             # End-to-end tests
├── docs/adr/                   # Architecture Decision Records
└── benchmarks/k6/              # Load test scripts
```

---

## Roadmap

- [x] **Phase 0** — Infrastructure (Docker Compose, Makefile)
- [x] **Phase 1** — Python Worker (PDF ETL, embeddings, Kafka consumer)
- [x] **Phase 2** — Go API Gateway (upload, vector search, LLM)
- [x] **Phase 3** — Semantic Cache (exact hash + vector similarity via Redis Stack)
- [x] **Phase 4** — Access Control (SpiceDB schema, Go integration, Qdrant filters)
- [x] **Phase 5** — Parallel Orchestration (goroutines + errgroup)
- [x] **Phase 6** — Testing, ADRs, Demo UI
- [ ] **Phase 7** — Observability (Prometheus, Grafana, Jaeger, OpenTelemetry)
- [ ] **Phase 8** — Benchmarks (K6: monolithic Python vs polyglot Go+Python)
- [ ] **Phase 9** — Research Paper

---

## Architecture Decisions

See [`docs/adr/`](docs/adr/) for detailed ADRs. Key decisions:

- **Redpanda over Kafka** — No JVM, no Zookeeper, ~512MB RAM vs ~2GB
- **Qdrant over Pinecone/Milvus** — Payload filtering for permissions, self-hosted
- **SpiceDB over custom RBAC** — Google Zanzibar model, gRPC-native
- **Fiber over Gin** — Faster benchmarks, Express-like API
- **MinIO over local FS** — S3-compatible, same code in dev and prod

---

## License

MIT
