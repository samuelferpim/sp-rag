# =============================================================
# SP-RAG Python Worker
# =============================================================
# Build:  docker build -t sp-rag-worker .
# Run:    docker run --env-file ../../.env sp-rag-worker
# =============================================================

FROM python:3.12-slim

# System dependencies required by unstructured[pdf]:
#   libmagic1   — MIME-type detection
#   poppler-utils — PDF → image/text rendering (pdfinfo, pdftotext)
#   tesseract-ocr — OCR fallback for scanned PDFs
RUN apt-get update && apt-get install -y --no-install-recommends \
    libmagic1 \
    poppler-utils \
    tesseract-ocr \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Install Python dependencies before copying source code
# so that unchanged deps benefit from Docker layer cache.
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application source
COPY app/ app/

# Run as non-root for security
RUN useradd --no-create-home --shell /bin/false worker
USER worker

# python -m app  →  runs app/__main__.py
CMD ["python", "-m", "app"]
