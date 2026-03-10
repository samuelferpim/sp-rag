# =============================================================
# SP-RAG Python Worker
# =============================================================
# Build:  docker build -t sp-rag-worker .
# Run:    docker run --env-file ../../.env sp-rag-worker
# =============================================================

FROM python:3.12-slim

# Force immediate logs & prevent .pyc clutter
ENV PYTHONUNBUFFERED=1
ENV PYTHONDONTWRITEBYTECODE=1
# Set a global writable directory for NLTK data
ENV NLTK_DATA=/usr/local/share/nltk_data

# System dependencies required by unstructured[pdf]:
#   libmagic1   — MIME-type detection
#   poppler-utils — PDF → image/text rendering (pdfinfo, pdftotext)
#   tesseract-ocr — OCR fallback for scanned PDFs
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1 \
    libglib2.0-0 \
    libmagic1 \
    poppler-utils \
    tesseract-ocr \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Install Python dependencies before copying source code
# so that unchanged deps benefit from Docker layer cache.
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Pre-download NLTK packages as root to avoid runtime permission issues
RUN python -m nltk.downloader -d $NLTK_DATA averaged_perceptron_tagger_eng punkt_tab

# Copy application source
COPY app/ app/

# Create a system user with a home directory so tools like 
# Matplotlib and Fontconfig have a place to write cache files.
RUN useradd -m worker && chown -R worker:worker /app
USER worker

# Execute the module
CMD ["python", "-m", "app"]