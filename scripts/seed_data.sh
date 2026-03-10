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
# Discover the Docker Compose internal network name
NETWORK=$(docker inspect sp-rag-minio -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{end}}' | head -n 1)

# Use the official MinIO Client (mc) image to upload via internal network
# We use --entrypoint sh because the image defaults to the 'mc' binary
docker run --rm -v "$PDF_PATH":/tmp/sample.pdf \
    --network "$NETWORK" \
    --entrypoint sh \
    minio/mc:RELEASE.2025-04-16T18-13-26Z \
    -c "mc alias set local http://sp-rag-minio:9000 sprag sprag12345 > /dev/null && mc cp /tmp/sample.pdf local/documents/sample.pdf > /dev/null"

echo -e "${YELLOW}▸${NC} Publishing event to Redpanda (Kafka)..."
# Generate current timestamp (ISO-8601)
NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Create the JSON payload. Simulating your user ("samuca").
PAYLOAD=$(echo '{"file_path": "sample.pdf", "file_name": "sample.pdf", "user_id": "samuca", "permissions": ["engineering_team", "admin"], "uploaded_at": "'$NOW'"}' | tr -d '\n')

# Envia para o Redpanda
echo -n "$PAYLOAD" | docker exec -i sp-rag-redpanda rpk topic produce document.uploaded

echo -e "${GREEN}✓${NC} Seeding completed successfully!"

# Send the message to the topic directly via the Redpanda container
echo "$PAYLOAD" | docker exec -i sp-rag-redpanda rpk topic produce document.uploaded

echo -e "${GREEN}✓${NC} Seeding completed successfully!"
echo "  (Check the logs in the terminal where 'make worker' is running)"