# ADR-009: MinIO for Object Storage

## Status
Accepted

## Context
The system needs object storage for uploaded PDF documents. The Go API Gateway stores files on upload, and the Python Worker retrieves them for processing. Requirements:
1. S3-compatible API (industry standard, widely supported SDKs)
2. Self-hosted for local development (no cloud dependency)
3. Code that works identically in development (local) and production (AWS S3)

## Decision
Use MinIO as the object storage backend.

MinIO provides an S3-compatible API, allowing the same Go and Python SDK code (`minio-go`, `boto3`/`minio-py`) to work against both MinIO locally and AWS S3 in production by simply changing the endpoint URL.

**Note:** MinIO stopped publishing official Docker images in October 2025, and the repository was archived in February 2026. We use the last stable release available. Future migration options include Chainguard's MinIO image or SeaweedFS as an alternative S3-compatible store.

## Alternatives Considered
- **Local filesystem:** Simplest option, but doesn't provide an S3-compatible API. Code would need conditional paths for local vs cloud storage, increasing complexity and reducing confidence in local testing.
- **AWS S3 directly:** Production-ready, but requires AWS credentials and internet access for local development. Adds cost and latency to the development workflow.
- **SeaweedFS:** S3-compatible, actively maintained, and lighter than MinIO. A viable future migration target, but less widely known and tested at evaluation time.

## Consequences
**Positive:**
- S3-compatible API ensures code portability between dev and production
- Single Docker container, easy to include in docker-compose
- Web console (port 9001) for visual file inspection during development
- Both Go (`minio-go`) and Python (`minio`) have official SDKs

**Negative:**
- MinIO Docker image availability is uncertain (archived repository)
- Additional container in the development stack
- Overkill for simple file storage in development (but the API compatibility is worth it)
