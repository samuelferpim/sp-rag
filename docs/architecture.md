# SP-RAG Architecture Diagrams

## 1. System Overview (High-Level)

```mermaid
graph TB
    Client([Client])

    subgraph Gateway["Go API Gateway"]
        direction TB
        Upload[POST /documents/upload]
        Query[POST /query]
    end

    subgraph Intelligence["Python Worker"]
        direction TB
        Consumer[Kafka Consumer]
        ETL[ETL Pipeline]
        Embedder[Embedding Generator]
    end

    subgraph Infrastructure
        direction TB
        Redpanda[(Redpanda<br/>Kafka)]
        MinIO[(MinIO<br/>S3 Storage)]
        Qdrant[(Qdrant<br/>Vector DB)]
        Redis[(Redis<br/>Semantic Cache)]
        SpiceDB[(SpiceDB<br/>AuthZ)]
        OpenAI[OpenAI API]
    end

    Client --> Gateway
    Upload -->|store file| MinIO
    Upload -->|publish event| Redpanda
    Query -->|check permissions| SpiceDB
    Query -->|permission-aware cache| Redis
    Query -->|vector search| Qdrant
    Query -->|embed + generate| OpenAI

    Redpanda -->|document.uploaded| Consumer
    Consumer -->|download file| MinIO
    Consumer --> ETL --> Embedder
    Embedder -->|store vectors| Qdrant
    Embedder -->|document.processed| Redpanda

    style Gateway fill:#00ADD8,color:#fff,stroke:#00ADD8
    style Intelligence fill:#3776AB,color:#fff,stroke:#3776AB
    style Infrastructure fill:#f5f5f5,stroke:#ccc
```

---

## 2. Ingestion Flow (Async — Upload to Vectors)

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant Go as Go Gateway
    participant S3 as MinIO
    participant MQ as Redpanda
    participant Py as Python Worker
    participant AI as OpenAI
    participant VDB as Qdrant

    User->>Go: POST /documents/upload (PDF)
    Go->>S3: Save file to bucket
    S3-->>Go: OK (file path)
    Go->>MQ: Publish "document.uploaded"
    Go-->>User: 202 Accepted (document_id)

    Note over MQ,Py: Async processing starts

    MQ->>Py: Consume "document.uploaded"
    Py->>S3: Download file
    S3-->>Py: PDF bytes

    rect rgb(240, 248, 255)
        Note over Py: ETL Pipeline (Smart Chunking)
        Py->>Py: Extract text (unstructured.io)
        Py->>Py: Smart chunk (sections, char-based)
    end

    Py->>AI: Generate embeddings
    AI-->>Py: Vectors (1536 dims)
    Py->>VDB: Upsert vectors + metadata
    VDB-->>Py: OK
    Py->>MQ: Publish "document.processed"
```

---

## 3. Query Flow (Sync — Question to Answer)

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant Go as Go Gateway
    participant AuthZ as SpiceDB
    participant AI as OpenAI
    participant Cache as Redis
    participant VDB as Qdrant

    User->>Go: POST /query { "query": "..." }

    par Phase 1 — Parallel Goroutines
        Go->>AuthZ: Check user permissions
        Go->>AI: Embed query → vector
        Go->>AI: Classify complexity (Router)
    end

    AuthZ-->>Go: permissions: ["finance_team"]
    AI-->>Go: query vector (1536 dims)
    AI-->>Go: complexity: "simples" | "complexa"

    Note over Go,Cache: Phase 2 — Permission-Aware Cache Lookup

    Go->>Cache: Search semantic cache (vector + permission hash)
    Cache-->>Go: cache miss

    Note over Go,VDB: Phase 3 — RAG Pipeline + Self-Reflection

    Go->>VDB: Vector search + permission filter
    VDB-->>Go: Top-K relevant chunks

    rect rgb(255, 248, 240)
        Note over Go: Build RAG Prompt
        Go->>Go: Select model (fast vs main)
    end

    Go->>AI: Chat completion (draft)
    AI-->>Go: Draft answer

    rect rgb(255, 240, 240)
        Note over Go,AI: Self-Reflection (LLM-as-a-Judge)
        Go->>AI: Evaluate: is answer grounded?
        AI-->>Go: {"is_grounded": true/false}
        Note over Go: If not grounded: rewrite + re-evaluate (max 2x)
    end

    Go->>Cache: Save grounded response (TTL 1h)
    Go-->>User: 200 OK { answer, sources, grounded }
```

---

## 4. Query Flow — Cache Hit (Fast Path)

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant Go as Go Gateway
    participant AuthZ as SpiceDB
    participant AI as OpenAI
    participant Cache as Redis

    User->>Go: POST /query { "query": "..." }

    par Phase 1 — Parallel Goroutines
        Go->>AuthZ: Check user permissions
        Go->>AI: Embed query → vector
        Go->>AI: Classify complexity (Router)
    end

    AuthZ-->>Go: permissions: ["finance_team"]
    AI-->>Go: query vector (1536 dims)
    AI-->>Go: complexity (unused on cache hit)

    Go->>Cache: Search permission-aware semantic cache
    Cache-->>Go: cache hit (matching vector + permission scope)

    Note over Go: No Qdrant, No LLM, No evaluation needed

    Go-->>User: 200 OK { answer, sources, cached: true }
```

---

## 5. SpiceDB Permission Model

```mermaid
graph LR
    subgraph Users
        Alice([Alice])
        Bob([Bob])
        Charlie([Charlie])
    end

    subgraph Teams
        Finance[finance_team]
        Eng[eng_team]
        HR[hr_team]
    end

    subgraph Documents
        Doc1[/Relatório Financeiro Q3/]
        Doc2[/Engineering Roadmap/]
        Doc3[/HR Policy 2026/]
    end

    Alice -->|member| Finance
    Bob -->|member| Eng
    Charlie -->|member| HR

    Finance -->|viewer| Doc1
    Eng -->|viewer| Doc2
    HR -->|viewer| Doc3

    Alice -.->|can view| Doc1
    Alice -.->|blocked| Doc2
    Bob -.->|blocked| Doc1
    Bob -.->|can view| Doc2

    style Alice fill:#4CAF50,color:#fff
    style Bob fill:#2196F3,color:#fff
    style Charlie fill:#FF9800,color:#fff
    style Doc1 fill:#E8F5E9,stroke:#4CAF50
    style Doc2 fill:#E3F2FD,stroke:#2196F3
    style Doc3 fill:#FFF3E0,stroke:#FF9800
```

---

## 6. Infrastructure (Docker Compose)

```mermaid
graph TB
    subgraph Docker["Docker Compose — Network: sprag"]
        direction TB

        subgraph Data["Data Layer"]
            Qdrant["Qdrant v1.17.0<br/>:6333 :6334"]
            Redis["Redis 7.4<br/>:6379"]
            PG["Postgres 16<br/>(SpiceDB store)"]
        end

        subgraph Messaging["Event Layer"]
            RP["Redpanda v24.3.1<br/>:9092"]
            RPC["Redpanda Console v3.5.3<br/>:8080"]
        end

        subgraph Storage["Storage Layer"]
            MinIO["MinIO<br/>:9000 :9001"]
            MinIOInit["minio-init<br/>(create bucket)"]
        end

        subgraph Auth["Auth Layer"]
            SpiceDB["SpiceDB v1.49.1<br/>:50051 :8443"]
            Migrate["spicedb-migrate<br/>(run once)"]
        end

        RPC -->|depends| RP
        MinIOInit -->|depends| MinIO
        Migrate -->|depends| PG
        SpiceDB -->|depends| Migrate
    end

    style Data fill:#E3F2FD,stroke:#1565C0
    style Messaging fill:#FFF3E0,stroke:#EF6C00
    style Storage fill:#E8F5E9,stroke:#2E7D32
    style Auth fill:#F3E5F5,stroke:#7B1FA2
```

---

## 7. Project Roadmap
```mermaid
graph LR
    subgraph P0["Phase 0 — Infra"]
        P0A[Docker Compose + Makefile]:::done
    end

    subgraph P1["Phase 1 — Python Worker"]
        P1A[ETL - PDF to text]:::active
        P1B[Chunking + Embeddings]:::todo
        P1C[Kafka Consumer wiring]:::todo
    end

    subgraph P2["Phase 2 — Go API"]
        P2A[Fiber skeleton + upload]:::todo
        P2B[Vector search]:::todo
        P2C[LLM integration]:::todo
    end

    subgraph P3["Phase 3 — Cache"]
        P3A[Exact hash cache]:::todo
        P3B[Semantic similarity cache]:::todo
    end

    subgraph P4["Phase 4 — AuthZ"]
        P4A[SpiceDB schema + seed]:::todo
        P4B[Go integration]:::todo
        P4C[Qdrant filter optimization]:::todo
    end

    subgraph P5["Phase 5 — Performance"]
        P5A[Goroutines + errgroup]:::todo
    end

    subgraph P6["Phase 6 — Observability"]
        P6A[Prometheus + Grafana + Jaeger]:::todo
    end

    subgraph P7["Phase 7 — Benchmarks"]
        P7A[Python baseline + K6 tests]:::todo
    end

    subgraph P8["Phase 8 — Paper"]
        P8A[Research paper]:::todo
    end

    P0 --- P1 --- P2 --- P3
    P3 --- P4 --- P5 --- P6
    P6 --- P7 --- P8

    classDef done fill:#4CAF50,color:#fff,stroke:#388E3C
    classDef active fill:#FF9800,color:#fff,stroke:#F57C00
    classDef todo fill:#E0E0E0,color:#666,stroke:#BDBDBD
```