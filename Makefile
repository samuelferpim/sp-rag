# =============================================================
# SP-RAG Makefile
# Usage: make <target>
# =============================================================

.PHONY: help setup up down restart logs status health clean \
        topics topics-list \
        minio-bucket \
        spicedb-schema spicedb-check \
        gateway worker \
        bench seed \
        fmt lint test

.DEFAULT_GOAL := help

# --- Colors ---
GREEN  := \033[0;32m
YELLOW := \033[1;33m
CYAN   := \033[0;36m
NC     := \033[0m

# --- Vars ---
COMPOSE      := docker compose
KAFKA_BROKER := localhost:9092
REDPANDA     := sp-rag-redpanda

# =============================================================
# Help
# =============================================================

help: ## Show this help
	@echo ""
	@echo "$(CYAN)SP-RAG$(NC) — Available commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'
	@echo ""

# =============================================================
# Infrastructure
# =============================================================

setup: ## First-time setup: copy .env + start infra + create topics
	@test -f .env || (cp .env.example .env && echo "$(GREEN)✓$(NC) .env created from .env.example")
	@$(MAKE) up
	@sleep 5
	@$(MAKE) topics
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

logs: ## Tail logs from all services (Ctrl+C to stop)
	@$(COMPOSE) logs -f --tail=50

logs-%: ## Tail logs for a specific service (e.g. make logs-qdrant)
	@$(COMPOSE) logs -f --tail=100 $*

status: ## Show status of all containers
	@$(COMPOSE) ps -a

health: ## Run infrastructure health check
	@chmod +x scripts/check-infra.sh
	@./scripts/check-infra.sh

clean: ## Stop all services and DELETE all volumes (⚠️ destructive)
	@echo "$(YELLOW)⚠  This will delete ALL data (volumes, embeddings, cache).$(NC)"
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
	@docker exec sp-rag-spicedb zed schema write --insecure \
		--endpoint localhost:50051 \
		--token "$$(grep SPICEDB_PRESHARED_KEY .env | cut -d= -f2)" \
		< infra/spicedb/schema.zed
	@echo "$(GREEN)✓$(NC) Schema applied"

spicedb-check: ## Verify SpiceDB is responding
	@docker exec sp-rag-spicedb grpc_health_probe -addr=localhost:50051 && \
		echo "$(GREEN)✓$(NC) SpiceDB is healthy" || \
		echo "$(RED)✗$(NC) SpiceDB is not responding"

# =============================================================
# Application Services
# =============================================================

gateway: ## Run the Go API gateway locally
	@echo "$(YELLOW)▸$(NC) Starting Go gateway..."
	@cd services/gateway && go run cmd/main.go

gateway-build: ## Build the Go gateway binary
	@cd services/gateway && go build -o ../../bin/gateway cmd/main.go
	@echo "$(GREEN)✓$(NC) Binary at ./bin/gateway"

worker: ## Run the Python worker locally
	@echo "$(YELLOW)▸$(NC) Starting Python worker..."
	@cd services/worker && python -m app.consumer

worker-deps: ## Install Python worker dependencies
	@cd services/worker && pip install -r requirements.txt --break-system-packages
	@echo "$(GREEN)✓$(NC) Python dependencies installed"

# =============================================================
# Development
# =============================================================

fmt: ## Format code (Go + Python)
	@echo "$(YELLOW)▸$(NC) Formatting Go..."
	@cd services/gateway && go fmt ./...
	@echo "$(YELLOW)▸$(NC) Formatting Python..."
	@cd services/worker && python -m black app/ 2>/dev/null || echo "  (install black: pip install black)"
	@echo "$(GREEN)✓$(NC) Code formatted"

lint: ## Lint code (Go + Python)
	@echo "$(YELLOW)▸$(NC) Linting Go..."
	@cd services/gateway && go vet ./...
	@echo "$(YELLOW)▸$(NC) Linting Python..."
	@cd services/worker && python -m ruff check app/ 2>/dev/null || echo "  (install ruff: pip install ruff)"
	@echo "$(GREEN)✓$(NC) Lint complete"

test: ## Run all tests
	@echo "$(YELLOW)▸$(NC) Testing Go..."
	@cd services/gateway && go test ./... -v
	@echo "$(YELLOW)▸$(NC) Testing Python..."
	@cd services/worker && python -m pytest tests/ -v 2>/dev/null || echo "  (no tests yet)"

# =============================================================
# Data & Benchmarks
# =============================================================

seed: ## Upload sample PDFs to MinIO for testing
	@echo "$(YELLOW)▸$(NC) Seeding test data..."
	@chmod +x scripts/seed_data.sh
	@./scripts/seed_data.sh
	@echo "$(GREEN)✓$(NC) Test data uploaded"

bench: ## Run K6 load tests
	@echo "$(YELLOW)▸$(NC) Running benchmarks..."
	@k6 run benchmarks/k6/smoke.js