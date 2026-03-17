#!/bin/bash
# =============================================================
# SP-RAG Seed Data Script
# Downloads a PDF, uploads it to MinIO, and publishes
# a message to Kafka to wake up the Python Worker.
# =============================================================

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}▸${NC} Preparing a test PDF file..."
# Create temporary directory
mkdir -p /tmp/sprag-seed
PDF_PATH="/tmp/sprag-seed/sample.pdf"

# Download an official W3C dummy PDF
if [ ! -f "$PDF_PATH" ]; then
    curl -s https://bitcoin.org/bitcoin.pdf -o "$PDF_PATH"
fi

echo -e "${YELLOW}▸${NC} Uploading the file to MinIO (bucket: documents)..."
# Copy file into MinIO container and use mc inside it (avoids Colima bind mount issues)
docker cp "$PDF_PATH" sp-rag-minio:/tmp/sample.pdf
docker exec sp-rag-minio mc alias set local http://localhost:9000 sprag sprag12345 > /dev/null 2>&1
docker exec sp-rag-minio mc cp /tmp/sample.pdf local/documents/sample.pdf > /dev/null 2>&1

echo -e "${YELLOW}▸${NC} Publishing event to Redpanda (Kafka)..."
# Generate current timestamp (ISO-8601)
NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Create the JSON payload. Simulating your user ("samuca").
PAYLOAD=$(echo '{"file_path": "sample.pdf", "file_name": "sample.pdf", "user_id": "samuca", "permissions": ["engineering_team", "admin"], "uploaded_at": "'$NOW'"}' | tr -d '\n')

# Publish to Redpanda
echo "$PAYLOAD" | docker exec -i sp-rag-redpanda rpk topic produce document.uploaded

echo -e "${GREEN}✓${NC} Seeding completed successfully!"
echo "  (Check the logs in the terminal where 'make worker' is running)"