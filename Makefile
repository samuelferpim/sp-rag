# =============================================================
# SP-RAG Makefile
# Usage: make <target>
# =============================================================

.PHONY: help setup up down restart logs status health clean \
        topics topics-list \
        spicedb-schema spicedb-seed spicedb-check \
        gateway gateway-build gateway-stop worker worker-deps \
        fmt lint test test-go test-python test-e2e test-all \
        seed bench \
        redis-flush redis-cli ps

.DEFAULT_GOAL := help

# --- Colors ---
GREEN  := \033[0;32m
RED    := \033[0;31m
YELLOW := \033[1;33m
CYAN   := \033[0;36m
NC     := \033[0m

# --- Vars ---
# Auto-detect docker compose variant (plugin v2 vs standalone binary)
COMPOSE := $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo "docker-compose")
KAFKA_BROKER := localhost:9092
REDPANDA     := sp-rag-redpanda

# =============================================================
# Help
# =============================================================

help: ## Show this help
	@echo ""
	@echo "$(CYAN)SP-RAG$(NC) — Available commands:"
	@echo "$(CYAN)Using:$(NC) $(COMPOSE)"
	@echo ""
	@grep -E '^[a-zA-Z_%-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'
	@echo ""

# =============================================================
# Infrastructure
# =============================================================

setup: ## First-time setup: .env + infra + topics + SpiceDB schema/seed
	@test -f .env || (cp .env.example .env && echo "$(GREEN)✓$(NC) .env created from .env.example")
	@$(MAKE) up
	@echo "$(YELLOW)▸$(NC) Waiting for services to stabilize..."
	@sleep 8
	@$(MAKE) topics
	@$(MAKE) spicedb-seed
	@$(MAKE) health

up: ## Start all infrastructure services
	@echo "$(YELLOW)▸$(NC) Starting infrastructure..."
	@$(COMPOSE) up -d
	@echo "$(GREEN)✓$(NC) Infrastructure is up"

down: ## Stop all services
	@echo "$(YELLOW)▸$(NC) Stopping infrastructure..."
	@$(COMPOSE) down
	@echo "$(GREEN)✓$(NC) Infrastructure stopped"

restart: ## Restart all services
	@$(MAKE) down
	@$(MAKE) up

restart-%: ## Restart a specific service (e.g. make restart-gateway)
	@echo "$(YELLOW)▸$(NC) Restarting $*..."
	@$(COMPOSE) restart $*
	@echo "$(GREEN)✓$(NC) $* restarted"

logs: ## Tail logs from all services (Ctrl+C to stop)
	@$(COMPOSE) logs -f --tail=50

logs-%: ## Tail logs for a specific service (e.g. make logs-qdrant)
	@$(COMPOSE) logs -f --tail=100 $*

status: ## Show status of all containers
	@$(COMPOSE) ps -a

ps: ## Compact container status (name + state + ports)
	@$(COMPOSE) ps --format "table {{.Name}}\t{{.Status}}\t{{.Ports}}"

health: ## Run infrastructure health check
	@chmod +x scripts/check-infra.sh
	@./scripts/check-infra.sh

clean: ## Stop all services and DELETE all volumes (destructive)
	@echo "$(RED)This will delete ALL data (volumes, embeddings, cache).$(NC)"
	@read -p "Are you sure? [y/N] " confirm && [ "$$confirm" = "y" ] || exit 1
	@$(COMPOSE) down -v --remove-orphans
	@echo "$(GREEN)✓$(NC) Everything wiped clean"

# =============================================================
# Kafka / Redpanda Topics
# =============================================================

topics: ## Create Kafka topics for the project
	@echo "$(YELLOW)▸$(NC) Creating Kafka topics..."
	@docker exec $(REDPANDA) rpk topic create document.uploaded \
		--partitions 3 --replicas 1 2>/dev/null || true
	@docker exec $(REDPANDA) rpk topic create document.processed \
		--partitions 3 --replicas 1 2>/dev/null || true
	@docker exec $(REDPANDA) rpk topic create document.failed \
		--partitions 1 --replicas 1 2>/dev/null || true
	@echo "$(GREEN)✓$(NC) Topics ready"

topics-list: ## List all Kafka topics
	@docker exec $(REDPANDA) rpk topic list

# =============================================================
# SpiceDB (Authorization)
# =============================================================

spicedb-schema: ## Write the permission schema to SpiceDB
	@echo "$(YELLOW)▸$(NC) Writing SpiceDB schema..."
	@SCHEMA=$$(cat infra/spicedb/schema.zed) && \
		TOKEN=$$(grep SPICEDB_PRESHARED_KEY .env | cut -d= -f2) && \
		PAYLOAD=$$(jq -n --arg schema "$$SCHEMA" '{schema: $$schema}') && \
		curl -sf -X POST "http://localhost:$${SPICEDB_HTTP_PORT:-8443}/v1/schema/write" \
			-H "Authorization: Bearer $$TOKEN" \
			-H "Content-Type: application/json" \
			-d "$$PAYLOAD" > /dev/null
	@echo "$(GREEN)✓$(NC) Schema applied"

spicedb-seed: ## Seed SpiceDB with test users, teams, and documents
	@echo "$(YELLOW)▸$(NC) Seeding SpiceDB..."
	@chmod +x scripts/seed_spicedb.sh
	@./scripts/seed_spicedb.sh

spicedb-check: ## Verify SpiceDB is responding
	@docker exec sp-rag-spicedb grpc_health_probe -addr=localhost:50051 > /dev/null 2>&1 && \
		echo "$(GREEN)✓$(NC) SpiceDB is healthy (gRPC)" || \
		(curl -sf http://localhost:8443/healthz > /dev/null 2>&1 && \
			echo "$(GREEN)✓$(NC) SpiceDB is healthy (HTTP)" || \
			echo "$(RED)✗$(NC) SpiceDB is not responding")

# =============================================================
# Application Services
# =============================================================

gateway: ## Run the Go API gateway locally
	@echo "$(YELLOW)▸$(NC) Starting Go gateway on :8081..."
	@cd services/gateway && go run cmd/main.go

gateway-build: ## Build the Go gateway binary
	@mkdir -p bin
	@cd services/gateway && go build -o ../../bin/gateway cmd/main.go
	@echo "$(GREEN)✓$(NC) Binary at ./bin/gateway"

gateway-stop: ## Stop the locally running gateway process
	@lsof -ti:8081 | xargs kill 2>/dev/null && \
		echo "$(GREEN)✓$(NC) Gateway stopped" || \
		echo "$(YELLOW)▸$(NC) No gateway process found on :8081"

worker: ## Run the Python worker locally
	@echo "$(YELLOW)▸$(NC) Starting Python worker..."
	@cd services/worker && python -m app.consumer

worker-deps: ## Install Python worker dependencies
	@echo "$(YELLOW)▸$(NC) Installing Python dependencies..."
	@pip install -r services/worker/requirements.txt
	@echo "$(GREEN)✓$(NC) Python dependencies installed"

# =============================================================
# Redis
# =============================================================

redis-flush: ## Flush all Redis data (cache reset)
	@docker exec sp-rag-redis redis-cli FLUSHALL > /dev/null && \
		echo "$(GREEN)✓$(NC) Redis cache flushed" || \
		echo "$(RED)✗$(NC) Failed to flush Redis"

redis-cli: ## Open interactive Redis CLI
	@docker exec -it sp-rag-redis redis-cli

# =============================================================
# Development
# =============================================================

fmt: ## Format code (Go + Python)
	@echo "$(YELLOW)▸$(NC) Formatting Go..."
	@cd services/gateway && go fmt ./...
	@echo "$(YELLOW)▸$(NC) Formatting Python..."
	@cd services/worker && python -m black app/ tests/ 2>/dev/null || echo "  (install black: pip install black)"
	@echo "$(GREEN)✓$(NC) Code formatted"

lint: ## Lint code (Go + Python)
	@echo "$(YELLOW)▸$(NC) Linting Go..."
	@cd services/gateway && go vet ./...
	@echo "$(YELLOW)▸$(NC) Linting Python..."
	@cd services/worker && python -m ruff check app/ 2>/dev/null || echo "  (install ruff: pip install ruff)"
	@echo "$(GREEN)✓$(NC) Lint complete"

test: test-go test-python ## Run all unit tests (Go + Python)

test-go: ## Run Go unit tests
	@echo "$(YELLOW)▸$(NC) Testing Go..."
	@cd services/gateway && go test ./... -v -count=1

test-python: ## Run Python unit tests
	@echo "$(YELLOW)▸$(NC) Testing Python..."
	@cd services/worker && python -m pytest tests/ -v

test-e2e: ## Run E2E tests (requires infra + gateway + worker running)
	@echo "$(YELLOW)▸$(NC) Running E2E tests..."
	@chmod +x scripts/e2e_test.sh
	@./scripts/e2e_test.sh

test-all: test-go test-python test-e2e ## Run all tests (unit + E2E)

# =============================================================
# Data & Benchmarks
# =============================================================

seed: ## Upload sample PDFs to MinIO and trigger processing
	@echo "$(YELLOW)▸$(NC) Seeding test data..."
	@chmod +x scripts/seed_data.sh
	@./scripts/seed_data.sh

bench: ## Run K6 load tests
	@test -d benchmarks/k6 || (echo "$(RED)✗$(NC) benchmarks/k6/ not found (Phase 7)" && exit 1)
	@echo "$(YELLOW)▸$(NC) Running benchmarks..."
	@k6 run benchmarks/k6/smoke.js
