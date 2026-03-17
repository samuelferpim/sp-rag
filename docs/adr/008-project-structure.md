# ADR-008: Monorepo with Domain Separation

## Status
Accepted

## Context
The project contains two services (Go API Gateway and Python Worker) that share configuration, infrastructure definitions, and documentation. We need a repository structure that:
1. Keeps both services in a single repository for development convenience
2. Maintains clear separation between Go and Python codebases
3. Allows independent Docker builds and CI/CD pipelines
4. Follows language-specific conventions (Go's `internal/` pattern, Python's module structure)

## Decision
Use a monorepo with the following structure:

```
sp-rag/
├── services/
│   ├── gateway/           # Go API
│   │   ├── cmd/main.go    # Entry point
│   │   ├── internal/      # Non-exportable packages
│   │   │   ├── handler/   # HTTP handlers
│   │   │   ├── cache/     # Redis semantic cache
│   │   │   ├── authz/     # SpiceDB integration
│   │   │   ├── rag/       # Prompt building + LLM
│   │   │   ├── orchestrator/ # Parallel pipeline
│   │   │   ├── middleware/ # CORS, logging
│   │   │   └── config/    # Environment config
│   │   └── Dockerfile
│   └── worker/            # Python Worker
│       ├── app/
│       │   ├── consumer.py # Kafka consumer loop
│       │   ├── etl.py      # PDF extraction + chunking
│       │   ├── embedder.py # OpenAI embeddings → Qdrant
│       │   └── config.py   # Environment config
│       └── Dockerfile
├── infra/                 # Infrastructure configs
├── scripts/               # Operational scripts
├── docs/                  # Documentation + ADRs
└── docker-compose.yml     # Full stack definition
```

Key conventions:
- Go uses `internal/` to prevent external package imports
- Python uses `app/` with small, focused modules (no frameworks)
- Each service has its own Dockerfile for independent builds
- Shared infrastructure lives at the repository root

## Alternatives Considered
- **Separate repositories (polyrepo):** Clean separation, but adds overhead for shared configuration changes, cross-service PRs, and local development setup.
- **Go workspace with embedded Python:** Using Go's workspace feature, but Python doesn't integrate well with Go's module system.

## Consequences
**Positive:**
- Single `git clone` and `make setup` to get everything running
- Shared `docker-compose.yml` ensures consistent infrastructure
- Atomic commits can span both services when needed
- Single CI/CD pipeline can test integration between services

**Negative:**
- Larger repository size as the project grows
- Need discipline to maintain service boundaries (no direct imports between services)
- Go and Python tooling must coexist (separate linters, formatters, test runners)
