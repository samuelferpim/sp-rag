# SP-RAG Gateway — API Usage

Base URL: `http://localhost:8081/api/v1`

---

## Health Check

```bash
curl http://localhost:8081/api/v1/health
```

**Response:**
```json
{
  "status": "ok",
  "service": "sp-rag-gateway"
}
```

---

## Upload Document

Upload a PDF for processing (text extraction, chunking, embedding).

```bash
curl -X POST http://localhost:8081/api/v1/documents/upload \
  -F "file=@./my-report.pdf" \
  -F "user_id=user_123" \
  -F "permissions=finance_team,admin"
```

**Parameters (multipart/form-data):**

| Field         | Type   | Required | Description                          |
|---------------|--------|----------|--------------------------------------|
| `file`        | file   | yes      | PDF file to upload                   |
| `user_id`     | string | yes      | ID of the uploading user             |
| `permissions` | string | no       | Comma-separated list of groups/roles |

**Response (202 Accepted):**
```json
{
  "document_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "file_name": "my-report.pdf",
  "file_path": "documents/my-report.pdf",
  "message": "Document uploaded and queued for processing",
  "status": "processing"
}
```

The document is stored in MinIO and a `document.uploaded` Kafka event is published. The Python worker picks it up, extracts text, generates embeddings, and stores them in Qdrant.

---

## Query Documents

Ask a question over your uploaded documents.

```bash
curl -X POST http://localhost:8081/api/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What were the financial results for Q4?",
    "user_id": "alice",
    "top_k": 5
  }'
```

**Request Body (JSON):**

| Field     | Type   | Required | Default | Description                  |
|-----------|--------|----------|---------|------------------------------|
| `query`   | string | yes      | —       | Natural language question    |
| `user_id` | string | yes      | —       | ID of the querying user      |
| `top_k`   | int    | no       | `5`     | Number of chunks to retrieve |

> **Note:** Permissions are resolved automatically via SpiceDB based on the user's team memberships. The user does not need to provide permissions — the system enforces Zero-Trust access control.

**Response (200 OK):**
```json
{
  "answer": "According to the Q4 financial report, revenue increased by 15% year-over-year, reaching $2.3M. Operating expenses were reduced by 8% through automation initiatives (Source: financial-report-2025.pdf, Page 12).",
  "sources": [
    {
      "file_name": "financial-report-2025.pdf",
      "file_path": "documents/financial-report-2025.pdf",
      "page": 12,
      "score": 0.91,
      "snippet": "Q4 revenue reached $2.3M, representing a 15% increase over the same period..."
    },
    {
      "file_name": "financial-report-2025.pdf",
      "file_path": "documents/financial-report-2025.pdf",
      "page": 14,
      "score": 0.87,
      "snippet": "Operating expenses were reduced by 8% through automation initiatives across..."
    }
  ],
  "model": "gpt-4o-mini",
  "cached": false,
  "timing": {
    "embed_ms": 180,
    "authz_ms": 8,
    "cache_ms": 3,
    "qdrant_ms": 45,
    "llm_ms": 520,
    "total_ms": 756
  }
}
```

**Timing breakdown:** The `timing` field shows latency of each pipeline stage in milliseconds. `embed` and `authz` run in parallel (Phase 1), so total is less than the sum.

**How it works:**
1. The query is embedded via OpenAI (`text-embedding-3-small`)
2. **Semantic cache** check: searches Redis for a similar query vector with matching permission hash (threshold: 0.92)
3. **Exact cache** fallback: looks up SHA-256(normalized query + permission hash) in Redis
4. Cache miss: Qdrant vector search filtered by user permissions
5. Retrieved chunks assembled into a RAG prompt
6. OpenAI (`gpt-4o-mini`) generates the answer
7. Response saved to both caches (exact + semantic) with TTL (default: 1h)

**Cache behavior:**
- The `cached` field in the response indicates whether the answer came from cache
- Cache is **permission-aware**: the same query from users with different permissions will NOT share cache entries (Zero-Trust)
- Semantic cache catches paraphrased queries ("Q4 revenue?" vs "What was the revenue in Q4?")
- Exact cache catches identical queries instantly (no embedding needed for lookup)
- TTL and similarity threshold are configurable via env vars

---

## Error Responses

All errors follow the same format:

```json
{
  "error": "description of what went wrong"
}
```

| Status | Meaning                                    |
|--------|--------------------------------------------|
| 400    | Missing or invalid field (query, user_id…) |
| 500    | Internal error (MinIO, Kafka, Qdrant, LLM) |

---

## End-to-End Example

```bash
# 1. Start infrastructure
make up

# 2. Seed SpiceDB with test users and teams
make spicedb-seed

# 3. Start the gateway (or use Docker)
make gateway

# 4. Upload a document (creates SpiceDB relationships automatically)
curl -X POST http://localhost:8081/api/v1/documents/upload \
  -F "file=@./docs/bitcoin.pdf" \
  -F "user_id=bob" \
  -F "permissions=eng_team"

# 5. Wait for the worker to process (check logs)
make logs-worker

# 6. Query as bob (eng_team member, has access)
curl -s -X POST http://localhost:8081/api/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is the main concept described in the document?",
    "user_id": "bob"
  }' | jq .

# 7. Query as alice (NOT in eng_team, should get no results)
curl -s -X POST http://localhost:8081/api/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What is the main concept described in the document?",
    "user_id": "alice"
  }' | jq .
```
