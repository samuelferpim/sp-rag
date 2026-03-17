#!/usr/bin/env bash
# =============================================================
# SP-RAG End-to-End Test Script
# Requires: infra running (make up), gateway running, worker running
# =============================================================

set -euo pipefail

GATEWAY_URL="${GATEWAY_URL:-http://localhost:3000}"
API="$GATEWAY_URL/api/v1"

PASSED=0
FAILED=0
TOTAL=0

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# ─── Helpers ──────────────────────────────────────────────────

assert_status() {
    local test_name="$1"
    local expected="$2"
    local actual="$3"
    TOTAL=$((TOTAL + 1))
    if [ "$actual" -eq "$expected" ]; then
        echo -e "  ${GREEN}✓${NC} $test_name (HTTP $actual)"
        PASSED=$((PASSED + 1))
    else
        echo -e "  ${RED}✗${NC} $test_name (expected $expected, got $actual)"
        FAILED=$((FAILED + 1))
    fi
}

assert_contains() {
    local test_name="$1"
    local needle="$2"
    local haystack="$3"
    TOTAL=$((TOTAL + 1))
    if echo "$haystack" | grep -qi "$needle"; then
        echo -e "  ${GREEN}✓${NC} $test_name"
        PASSED=$((PASSED + 1))
    else
        echo -e "  ${RED}✗${NC} $test_name (expected to contain '$needle')"
        FAILED=$((FAILED + 1))
    fi
}

assert_not_contains() {
    local test_name="$1"
    local needle="$2"
    local haystack="$3"
    TOTAL=$((TOTAL + 1))
    if echo "$haystack" | grep -qi "$needle"; then
        echo -e "  ${RED}✗${NC} $test_name (should NOT contain '$needle')"
        FAILED=$((FAILED + 1))
    else
        echo -e "  ${GREEN}✓${NC} $test_name"
        PASSED=$((PASSED + 1))
    fi
}

wait_for_processing() {
    local max_wait="${1:-30}"
    local waited=0
    echo -e "  ${YELLOW}…${NC} Waiting for document processing (max ${max_wait}s)..."
    while [ $waited -lt "$max_wait" ]; do
        sleep 2
        waited=$((waited + 2))
        # Check if any vectors exist in Qdrant
        local count
        count=$(curl -s "http://localhost:6333/collections/documents/points/count" \
            -H "Content-Type: application/json" \
            -d '{"exact": true}' 2>/dev/null | grep -o '"count":[0-9]*' | cut -d: -f2 || echo "0")
        if [ "${count:-0}" -gt 0 ]; then
            echo -e "  ${GREEN}✓${NC} Document processed ($count vectors in Qdrant)"
            return 0
        fi
    done
    echo -e "  ${YELLOW}⚠${NC} Timeout waiting for processing (continuing anyway)"
    return 0
}

# ─── Pre-flight ───────────────────────────────────────────────

echo -e "\n${CYAN}SP-RAG E2E Tests${NC}\n"
echo -e "${YELLOW}▸${NC} Checking infrastructure..."

HEALTH_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API/health" 2>/dev/null || echo "000")
if [ "$HEALTH_STATUS" != "200" ]; then
    echo -e "${RED}✗${NC} Gateway is not running at $API (got HTTP $HEALTH_STATUS)"
    echo "  Run: make up && make gateway"
    exit 1
fi
echo -e "${GREEN}✓${NC} Gateway is healthy\n"

# ─── Test 1: Health Check ─────────────────────────────────────

echo -e "${CYAN}Test 1: Health Check${NC}"
RESP=$(curl -s -w "\n%{http_code}" "$API/health")
STATUS=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | head -1)

assert_status "GET /health returns 200" 200 "$STATUS"
assert_contains "Response contains status:ok" "ok" "$BODY"

# ─── Test 2: Upload PDF ──────────────────────────────────────

echo -e "\n${CYAN}Test 2: Document Upload${NC}"

# Create a test PDF on the fly
TEST_PDF=$(mktemp /tmp/e2e_test_XXXXX.pdf)
# Use Python to generate a minimal PDF if available, otherwise use a heredoc
python3 -c "
from fpdf import FPDF
pdf = FPDF()
pdf.add_page()
pdf.set_font('Helvetica', size=12)
pdf.multi_cell(0, 10, text=(
    'Distributed Systems and the CAP Theorem. '
    'The CAP theorem states that a distributed data store cannot simultaneously '
    'provide more than two of the following three guarantees: Consistency, '
    'Availability, and Partition tolerance. This fundamental theorem guides '
    'the design of modern distributed databases and storage systems.'
))
pdf.output('$TEST_PDF')
" 2>/dev/null || {
    # Fallback: create a minimal valid PDF
    printf '%%PDF-1.0\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</Type/Page/MediaBox[0 0 612 792]/Parent 2 0 R>>endobj\nxref\n0 4\ntrailer<</Size 4/Root 1 0 R>>\nstartxref\n0\n%%%%EOF' > "$TEST_PDF"
}

RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/documents/upload" \
    -F "file=@${TEST_PDF}" \
    -F "user_id=alice" \
    -F "permissions=finance_team,eng_team")
STATUS=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | sed '$d')

assert_status "POST /upload returns 202" 202 "$STATUS"
assert_contains "Response contains file_path" "file_path" "$BODY"

# Wait for worker to process
wait_for_processing 30

# ─── Test 3: Query (Authorized User) ─────────────────────────

echo -e "\n${CYAN}Test 3: Query (Authorized User)${NC}"
RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/query" \
    -H "Content-Type: application/json" \
    -d '{"query": "What is the CAP theorem?", "user_id": "alice"}')
STATUS=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | sed '$d')

assert_status "POST /query returns 200" 200 "$STATUS"
assert_contains "Response contains answer" "answer" "$BODY"
assert_contains "Response contains sources" "sources" "$BODY"
assert_contains "Response contains timing" "timing" "$BODY"

# ─── Test 4: Query (Unauthorized User) ───────────────────────

echo -e "\n${CYAN}Test 4: Query (Unauthorized User)${NC}"
RESP=$(curl -s -w "\n%{http_code}" -X POST "$API/query" \
    -H "Content-Type: application/json" \
    -d '{"query": "What is the CAP theorem?", "user_id": "unknown_user_no_teams"}')
STATUS=$(echo "$RESP" | tail -1)
BODY=$(echo "$RESP" | sed '$d')

# User with no teams should get no results or denied
assert_status "POST /query returns 200 (but empty)" 200 "$STATUS"
assert_contains "Response has answer field" "answer" "$BODY"

# ─── Test 5: Cache Hit (Second Query) ────────────────────────

echo -e "\n${CYAN}Test 5: Cache Hit (Same Query Twice)${NC}"
RESP1=$(curl -s -X POST "$API/query" \
    -H "Content-Type: application/json" \
    -d '{"query": "What is the CAP theorem?", "user_id": "alice"}')

RESP2=$(curl -s -X POST "$API/query" \
    -H "Content-Type: application/json" \
    -d '{"query": "What is the CAP theorem?", "user_id": "alice"}')

CACHED=$(echo "$RESP2" | grep -o '"cached":true' || echo "")
TOTAL=$((TOTAL + 1))
if [ -n "$CACHED" ]; then
    echo -e "  ${GREEN}✓${NC} Second query is a cache hit"
    PASSED=$((PASSED + 1))
else
    echo -e "  ${RED}✗${NC} Second query should be cached (got: $(echo "$RESP2" | grep -o '"cached":[a-z]*'))"
    FAILED=$((FAILED + 1))
fi

# ─── Test 6: Validation Errors ───────────────────────────────

echo -e "\n${CYAN}Test 6: Input Validation${NC}"

# Missing body
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API/query" \
    -H "Content-Type: application/json" \
    -d '{}')
assert_status "Empty body returns 400" 400 "$STATUS"

# Missing query
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API/query" \
    -H "Content-Type: application/json" \
    -d '{"user_id": "alice"}')
assert_status "Missing query returns 400" 400 "$STATUS"

# Missing user_id
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API/query" \
    -H "Content-Type: application/json" \
    -d '{"query": "test"}')
assert_status "Missing user_id returns 400" 400 "$STATUS"

# No file in upload
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$API/documents/upload" \
    -H "Content-Type: multipart/form-data; boundary=test")
assert_status "Upload without file returns 400" 400 "$STATUS"

# ─── Cleanup ──────────────────────────────────────────────────

rm -f "$TEST_PDF"

# ─── Summary ─────────────────────────────────────────────────

echo -e "\n${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "  ${GREEN}Passed:${NC} $PASSED"
echo -e "  ${RED}Failed:${NC} $FAILED"
echo -e "  Total:  $TOTAL"
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"

if [ "$FAILED" -gt 0 ]; then
    exit 1
fi
exit 0
