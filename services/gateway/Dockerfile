# =============================================================
# SP-RAG Go API Gateway
# =============================================================
# Build:  docker build -t sp-rag-gateway .
# Run:    docker run --env-file ../../.env -p 8081:8081 sp-rag-gateway
# =============================================================

# --- Build stage ---
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o gateway cmd/main.go

# --- Runtime stage ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates

RUN adduser -D -h /app gateway
WORKDIR /app

COPY --from=builder /build/gateway .

USER gateway

EXPOSE 8081

CMD ["./gateway"]
