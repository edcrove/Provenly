# Provenly — developer and CI entry points. Run `make help` for the list.
SHELL := /bin/bash
.DEFAULT_GOAL := help

ROOT        := $(CURDIR)
OUT         := $(ROOT)/coverage/out
GOCOV       := $(OUT)/gocov
BACKEND_PKG := $$(go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./cmd/... ./internal/...)
# Environments (docker compose): demo | qa | prod, see docs/environments.md.
ENV         ?= demo
ENV_FILE    := envs/$(ENV).env
COMPOSE     := docker compose --env-file $(ENV_FILE)
DEV_COMPOSE := $(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml
TIMESTAMP   := $(shell date +%Y%m%d-%H%M%S)
# Optional CA for image builds behind a TLS-intercepting proxy.
export EXTRA_CA_CERT
# Local, non-docker runs (make dev-backend / migrate) use the qa database by default.
DATABASE_URL ?= postgres://provenly:provenly@localhost:5433/provenly?sslmode=disable

.PHONY: help setup up dev down ps logs infra db-dump db-reset db-restore demo-reset seed-snapshot seed-rebuild-demo \
	check-env guard-prod migrate dev-backend dev-frontend generate check-generated lint screenshots \
	test-backend-unit test-backend-integration test-backend-contract \
	test-frontend-unit test-frontend-integration test-frontend-contract test-e2e \
	test gates coverage clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-28s %s\n", $$1, $$2}'

setup: ## Install Go modules and npm dependencies (frontend, e2e)
	cd backend && go mod download
	cd tools/covgate && go mod download
	cd frontend && npm ci
	cd e2e && npm ci

check-env:
	@test -f $(ENV_FILE) || { echo "error: $(ENV_FILE) not found (prod: cp envs/prod.env.example envs/prod.env and set a password)" >&2; exit 1; }

guard-prod:
	@if [ "$(ENV)" = prod ] && [ "$(CONFIRM)" != prod ]; then echo "refusing to touch prod data: add CONFIRM=prod" >&2; exit 1; fi

up: check-env ## Build and start ENV=demo|qa|prod (postgres, seed, migrate, api, web); data persists
	$(COMPOSE) up -d --build --wait
	@$(COMPOSE) ps --format 'table {{.Service}}\t{{.Status}}\t{{.Ports}}'

dev: check-env ## Same as `up` with hot reload (api: air, web: Vite HMR) on ENV's data
	$(DEV_COMPOSE) up -d --build --wait

down: check-env ## Stop ENV, keeping its data
	$(COMPOSE) down

ps: check-env ## Show ENV's containers
	$(COMPOSE) ps

logs: check-env ## Follow ENV's logs (SERVICE=api to narrow)
	$(COMPOSE) logs -f $(SERVICE)

infra: check-env ## Start only ENV's database (seeded + migrated) for non-docker `make dev-backend`
	$(COMPOSE) up -d --wait postgres
	$(COMPOSE) up --build seed migrate

db-dump: check-env ## Dump ENV's database to backups/<env>-<timestamp>.sql
	@mkdir -p backups
	$(COMPOSE) exec -T postgres sh -c 'pg_dump -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" --no-owner --no-privileges' > backups/$(ENV)-$(TIMESTAMP).sql
	@echo "saved backups/$(ENV)-$(TIMESTAMP).sql"

db-reset: check-env guard-prod ## Delete ENV's data and start again from its seed (prod: CONFIRM=prod, dumps first)
	@if [ "$(ENV)" = prod ]; then $(MAKE) --no-print-directory db-dump ENV=prod; fi
	$(COMPOSE) down -v
	$(COMPOSE) up -d --wait

db-restore: check-env guard-prod ## Replace ENV's data with a dump: FILE=backups/<file>.sql (prod: CONFIRM=prod, dumps first)
	@test -f "$(FILE)" || { echo "error: FILE=<dump.sql> is required" >&2; exit 1; }
	@if [ "$(ENV)" = prod ]; then $(MAKE) --no-print-directory db-dump ENV=prod; fi
	$(COMPOSE) down -v
	$(COMPOSE) up -d --wait postgres
	$(COMPOSE) exec -T postgres sh -c 'psql -q -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' < "$(FILE)" >/dev/null
	$(COMPOSE) up -d --wait

demo-reset: ## Bring the demo environment back to the demo snapshot
	$(MAKE) --no-print-directory db-reset ENV=demo

seed-snapshot: ## Save environment FROM's data as a new seed: FROM=qa NAME=<name> -> seeds/<name>.sql
	@test -n "$(FROM)" -a -n "$(NAME)" || { echo "error: FROM=<env> NAME=<seed name> are required" >&2; exit 1; }
	@if [ "$(FROM)" = prod ] && [ "$(CONFIRM)" != prod ]; then echo "refusing to copy prod data into the repo: add CONFIRM=prod" >&2; exit 1; fi
	docker compose --env-file envs/$(FROM).env exec -T postgres sh -c 'pg_dump -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" --no-owner --no-privileges' > seeds/$(NAME).sql
	@echo "saved seeds/$(NAME).sql: set SEED=$(NAME) in an env file, then make db-reset ENV=<env>"

seed-rebuild-demo: ## Regenerate seeds/demo.sql from scripts/seed/demo-data.sh (resets demo)
	docker compose --env-file envs/demo.env down -v
	SEED= docker compose --env-file envs/demo.env up -d --build --wait
	./scripts/seed/demo-data.sh http://localhost:8080
	$(MAKE) --no-print-directory seed-snapshot FROM=demo NAME=demo

migrate: ## Apply goose migrations to $$DATABASE_URL
	cd backend && PROVENLY_DATABASE_URL='$(DATABASE_URL)' go run ./cmd/provenly migrate up

dev-backend: ## Run the API on :8080 (applies migrations on start)
	cd backend && PROVENLY_DATABASE_URL='$(DATABASE_URL)' PROVENLY_AUTO_MIGRATE=true go run ./cmd/provenly serve

dev-frontend: ## Run the UI on :5173 (proxies /api to :8080)
	cd frontend && npm run dev

generate: ## Regenerate sqlc queries and the OpenAPI TypeScript client
	cd backend && sqlc generate
	cd frontend && npm run gen:api

check-generated: ## Fail if generated code drifted from the SQL / OpenAPI sources
	cd backend && sqlc diff
	cd frontend && npm run gen:api && git diff --exit-code -- src/api/schema.d.ts

lint: ## Lint, vet, format-check and typecheck both services
	test -z "$$(gofmt -l backend tools)" || (gofmt -l backend tools && exit 1)
	cd backend && go vet ./... && go vet -tags integration,contract ./test/...
	cd backend && golangci-lint run ./... && golangci-lint run --build-tags integration,contract ./test/...
	cd tools/covgate && go vet ./...
	cd frontend && npm run lint && npm run format:check && npm run typecheck
	cd e2e && npm run typecheck

test-backend-unit: $(OUT) ## Backend Unit (testing+testify), raw coverage in GOCOVERDIR
	rm -rf $(GOCOV)/unit && mkdir -p $(GOCOV)/unit
	cd backend && go test -covermode=atomic -coverpkg=./... $(BACKEND_PKG) -args -test.gocoverdir=$(GOCOV)/unit
	cd backend && go tool covdata textfmt -i=$(GOCOV)/unit -o=$(OUT)/backend-unit.out

test-backend-integration: $(OUT) ## Backend Integration (testcontainers-go, real Postgres; needs Docker)
	rm -rf $(GOCOV)/integration && mkdir -p $(GOCOV)/integration
	cd backend && go test -tags integration -count=1 -json -covermode=atomic -coverpkg=./... ./test/integration/ \
		-args -test.gocoverdir=$(GOCOV)/integration > $(OUT)/backend-integration.json; status=$$?; \
		grep -o '"Action":"\(pass\|fail\)","Package":"[^"]*","Test":"[^"]*"' $(OUT)/backend-integration.json | sed 's/"Action":"//;s/","Package":"[^"]*","Test":"/  /;s/"$$//'; \
		exit $$status
	cd backend && go tool covdata textfmt -i=$(GOCOV)/integration -o=$(OUT)/backend-integration.out

test-backend-contract: $(OUT) ## Backend Contract (httpexpect + kin-openapi against the real API; needs Docker)
	rm -rf $(GOCOV)/contract && mkdir -p $(GOCOV)/contract
	cd backend && CONTRACT_EVIDENCE=$(OUT)/backend-contract.json go test -tags contract -count=1 -covermode=atomic \
		-coverpkg=./... ./test/contract/ -args -test.gocoverdir=$(GOCOV)/contract

test-frontend-unit: ## Frontend Unit (Vitest + @vitest/coverage-v8)
	cd frontend && npm run test:unit

test-frontend-integration: ## Frontend Integration (Vitest + Testing Library + MSW)
	cd frontend && npm run test:integration

test-frontend-contract: ## Frontend Contract (generated client + MSW handlers validated against the spec)
	cd frontend && npm run test:contract

test-e2e: ## E2E journeys (Playwright) on the instrumented stack and an ephemeral database
	./scripts/e2e.sh

test: test-backend-unit test-backend-integration test-backend-contract test-frontend-unit test-frontend-integration test-frontend-contract test-e2e ## Run every suite

screenshots: ## Capture every UI flow into docs/screenshots (ephemeral database)
	./scripts/screenshots.sh

gates: ## Evaluate the 8 coverage gates from the evidence already collected
	cd tools/covgate && go run . -root $(ROOT)

coverage: $(OUT) test gates ## Run every suite, then generate the 8 gate reports + consolidated evidence

$(OUT):
	mkdir -p $(OUT)

clean: ## Remove generated reports and build outputs
	rm -rf coverage/out frontend/coverage frontend/dist e2e/coverage e2e/test-results e2e/playwright-report backend/bin
