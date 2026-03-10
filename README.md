# 🛡️ Secure Polyglot RAG (SP-RAG)

![Golang](https://img.shields.io/badge/Golang-1.21+-00ADD8?style=for-the-badge&logo=go)
![Python](https://img.shields.io/badge/Python-3.11+-3776AB?style=for-the-badge&logo=python)
![Kafka](https://img.shields.io/badge/Apache_Kafka-231F20?style=for-the-badge&logo=apache-kafka)
![Qdrant](https://img.shields.io/badge/Qdrant-Vector_DB-FF5252?style=for-the-badge)
![SpiceDB](https://img.shields.io/badge/SpiceDB-Zero_Trust-4285F4?style=for-the-badge)

An experimental, high-performance **Retrieval-Augmented Generation (RAG)** architecture designed for enterprise environments.

SP-RAG solves three critical bottlenecks in modern Generative AI applications: **LLM latency, API costs, and data access governance**. By decoupling the heavy NLP ingestion pipeline (Python) from the high-concurrency API gateway and orchestration layer (Golang) via event-driven messaging, this system delivers secure, millisecond-level semantic searches.

### ✨ Key Features
* **Polyglot Microservices:** Python workers for data extraction (`unstructured.io`) and Golang API for blazingly fast request handling.
* **Zero-Trust AI (RBAC/ABAC):** Granular document-level access control using **SpiceDB** (Google Zanzibar model). The LLM only sees what the user is explicitly allowed to see.
* **Semantic Caching:** Drastically reduces OpenAI API costs and response times by caching similar queries in **Redis (RedisVL)**.
* **Event-Driven Ingestion:** Asynchronous document processing pipeline powered by **Apache Kafka**.

---

## 🏗️ Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                         Client (HTTP)                            │
└──────────────────────┬───────────────────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────────────────┐
│                   Go API Gateway (Fiber)                          │
│                                                                   │
│  ┌─────────┐  ┌──────────────┐  ┌───────────┐  ┌─────────────┐  │
│  │ AuthZ   │  │ Semantic     │  │ Vector    │  │ LLM         │  │
│  │ SpiceDB │  │ Cache Redis  │  │ Search    │  │ Orchestrator│  │
│  └────┬────┘  └──────┬───────┘  └─────┬─────┘  └──────┬──────┘  │
│       │              │                │               │          │
└───────┼──────────────┼────────────────┼───────────────┼──────────┘
        │              │                │               │
        ▼              ▼                ▼               ▼
   ┌─────────┐   ┌─────────┐     ┌──────────┐    ┌──────────┐
   │ SpiceDB │   │  Redis  │     │  Qdrant  │    │ OpenAI   │
   └─────────┘   └─────────┘     └──────────┘    │ API      │
                                       ▲          └──────────┘
                                       │
┌──────────────────────────────────────┼───────────────────────────┐
│              Python Worker           │                            │
│                                      │                            │
│  Kafka Consumer → ETL → Embeddings ──┘                            │
│  (unstructured)   (chunking)  (OpenAI)                            │
└──────────────────────────────────────────────────────────────────┘
        ▲                                        ▲
        │          ┌──────────────┐              │
        └──────────│  Redpanda    │──────────────┘
                   │  (Kafka)     │
                   └──────────────┘
                          ▲
                          │
                   ┌──────────────┐
                   │    MinIO     │
                   │  (S3 Storage)│
                   └──────────────┘
```

---

## 📦 Tech Stack

| Layer | Technology | Purpose |
|-------|-----------|---------|
| API Gateway | **Go** (Fiber) | High-concurrency orchestration, routing, caching |
| AI Worker | **Python** | Document ETL, embeddings, NLP processing |
| Message Broker | **Redpanda** | Kafka-compatible event streaming (no JVM/Zookeeper) |
| Vector Database | **Qdrant** | Semantic search with metadata payload filtering |
| Authorization | **SpiceDB** | Google Zanzibar-based RBAC/ABAC |
| Semantic Cache | **Redis** | Sub-millisecond query caching with vector similarity |
| Object Storage | **MinIO** | S3-compatible document storage |
| LLM | **OpenAI** (GPT-4o) | Response generation |

---

## 🚀 Quickstart

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) & Docker Compose
- [Go 1.21+](https://go.dev/dl/)
- [Python 3.11+](https://www.python.org/downloads/)
- An [OpenAI API key](https://platform.openai.com/api-keys)

### Setup

```bash
# Clone the repo
git clone https://github.com/samuelferpim/sp-rag.git
cd sp-rag

# One-command setup: creates .env, starts infra, creates Kafka topics, runs health check
make setup
```

That's it. All services will be running locally:

| Service | URL |
|---------|-----|
| Qdrant Dashboard | http://localhost:6333/dashboard |
| Redpanda Console | http://localhost:8080 |
| MinIO Console | http://localhost:9001 |
| SpiceDB gRPC | localhost:50051 |
| Redis | localhost:6379 |

---

## 🛠️ Makefile Commands

Run `make help` to see all available commands:

```
Infrastructure
  make setup             First-time setup (env + infra + topics + health check)
  make up                Start all services
  make down              Stop all services
  make restart           Restart all services
  make status            Show container status
  make health            Run infrastructure health check
  make clean             Stop services and delete all data (⚠️ destructive)

Kafka
  make topics            Create project Kafka topics
  make topics-list       List all topics

Application
  make gateway           Run Go API gateway locally
  make worker            Run Python worker locally
  make worker-deps       Install Python dependencies

Development
  make fmt               Format Go + Python code
  make lint              Lint Go + Python code
  make test              Run all tests
  make logs              Tail all service logs
  make logs-<service>    Tail logs for a specific service (e.g. make logs-qdrant)

Data & Benchmarks
  make seed              Upload sample PDFs for testing
  make bench             Run K6 load tests
```

---

## 📁 Project Structure

```
sp-rag/
├── docker-compose.yml          # All infrastructure services
├── Makefile                    # Project commands
├── .env.example                # Environment variables template
│
├── services/
│   ├── gateway/                # 🟦 Go API (Fiber)
│   │   ├── cmd/
│   │   │   └── main.go
│   │   ├── internal/
│   │   │   ├── handler/        # HTTP route handlers
│   │   │   ├── middleware/     # Auth, logging, CORS
│   │   │   ├── rag/            # Prompt building, LLM calls
│   │   │   ├── cache/          # Redis semantic cache
│   │   │   └── authz/          # SpiceDB integration
│   │   ├── go.mod
│   │   └── Dockerfile
│   │
│   └── worker/                 # 🟨 Python Worker
│       ├── app/
│       │   ├── consumer.py     # Kafka consumer loop
│       │   ├── etl.py          # PDF/DOCX text extraction
│       │   └── embedder.py     # OpenAI embeddings + Qdrant
│       ├── requirements.txt
│       └── Dockerfile
│
├── infra/
│   └── spicedb/
│       └── schema.zed          # Permission model (Zanzibar)
│
├── scripts/
│   ├── check-infra.sh          # Health check for all services
│   └── seed_data.sh            # Populate test data
│
├── benchmarks/
│   └── k6/                     # Load testing scripts
│
└── docs/
    └── architecture.md         # Detailed design decisions
```

---

## 🗺️ Roadmap

- [x] **Phase 0** — Infrastructure (Docker Compose, Makefile)
- [ ] **Phase 1** — Python Worker (PDF ETL → Embeddings → Qdrant)
- [ ] **Phase 2** — Go API Gateway (Upload + Vector Search + LLM)
- [ ] **Phase 3** — Semantic Cache (Redis)
- [ ] **Phase 4** — Access Control (SpiceDB)
- [ ] **Phase 5** — Parallel Orchestration (Goroutines + errgroup)
- [ ] **Phase 6** — Observability (Prometheus, Grafana, Jaeger)
- [ ] **Phase 7** — Benchmarks (K6 load tests, comparative analysis)
- [ ] **Phase 8** — Research Paper

---

## 📄 License

MIT