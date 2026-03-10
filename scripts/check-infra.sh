#!/bin/bash
# =============================================================
# SP-RAG Infrastructure Health Check
# Run after: docker compose up -d (or: make health)
# =============================================================

set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "  ${GREEN}✓${NC} $1"; }
fail() { echo -e "  ${RED}✗${NC} $1"; ERRORS=$((ERRORS + 1)); }
info() { echo -e "\n${YELLOW}▸${NC} $1"; }

ERRORS=0

echo ""
echo "========================================="
echo "  SP-RAG Infrastructure Health Check"
echo "========================================="

# --- Qdrant ---
info "Checking Qdrant (Vector DB)..."
if curl -sf http://localhost:6333/healthz > /dev/null 2>&1; then
  pass "Qdrant REST API on :6333"
else
  fail "Qdrant REST API not responding"
fi

# --- Redis ---
info "Checking Redis (Cache)..."
if docker exec sp-rag-redis redis-cli ping 2>/dev/null | grep -q PONG; then
  pass "Redis on :6379 → PONG"
else
  fail "Redis not responding"
fi

# --- Redpanda (Kafka) ---
info "Checking Redpanda (Kafka-compatible broker)..."
if docker exec sp-rag-redpanda rpk cluster info > /dev/null 2>&1; then
  pass "Redpanda cluster responding on :9092"
else
  fail "Redpanda not responding"
fi

# --- Redpanda Console ---
info "Checking Redpanda Console (UI)..."
if curl -sf http://localhost:8080 > /dev/null 2>&1; then
  pass "Redpanda Console on :8080"
else
  fail "Redpanda Console not responding"
fi

# --- MinIO ---
info "Checking MinIO (Object Storage)..."
if curl -sf http://localhost:9000/minio/health/live > /dev/null 2>&1; then
  pass "MinIO API on :9000"
else
  fail "MinIO API not responding"
fi

if curl -sf http://localhost:9001 > /dev/null 2>&1; then
  pass "MinIO Console on :9001"
else
  fail "MinIO Console not responding"
fi

# --- SpiceDB ---
info "Checking SpiceDB (Authorization)..."
if curl -sf http://localhost:8443/healthz > /dev/null 2>&1; then
  pass "SpiceDB HTTP on :8443"
else
  fail "SpiceDB not responding"
fi

# --- Python Worker ---
info "Checking Python Worker (ETL & Embeddings)..."
if [ "$(docker inspect -f '{{.State.Status}}' sp-rag-worker 2>/dev/null)" == "running" ]; then
  pass "Python Worker container is running"
else
  fail "Python Worker container is not running or crashed"
fi

# --- Summary ---
echo ""
echo "========================================="
if [ $ERRORS -eq 0 ]; then
  echo -e "  ${GREEN}All services are healthy!${NC}"
else
  echo -e "  ${RED}${ERRORS} service(s) failed.${NC}"
  echo "  Run 'docker compose logs <service>' to debug."
fi
echo "========================================="
echo ""

exit $ERRORS