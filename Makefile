# Provenly — developer and CI entry points. Run `make help` for the list.
SHELL := /bin/bash
.DEFAULT_GOAL := help

ROOT        := $(CURDIR)
OUT         := $(ROOT)/coverage/out
GOCOV       := $(OUT)/gocov
BACKEND_PKG := $$(go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./cmd/... ./internal/...)
DATABASE_URL ?= postgres://provenly:provenly@localhost:5432/provenly?sslmode=disable

.PHONY: help setup up down migrate dev-backend dev-frontend generate check-generated lint screenshots \
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

up: ## Start PostgreSQL (docker compose) and wait until healthy
	docker compose up -d --wait postgres

down: ## Stop local infrastructure
	docker compose down

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

test-e2e: ## E2E journeys (Playwright) on the instrumented stack; needs `make up`
	./scripts/e2e.sh

test: test-backend-unit test-backend-integration test-backend-contract test-frontend-unit test-frontend-integration test-frontend-contract test-e2e ## Run every suite

screenshots: ## Capture every UI flow into docs/screenshots (needs make up; resets the E2E database)
	./scripts/screenshots.sh

gates: ## Evaluate the 8 coverage gates from the evidence already collected
	cd tools/covgate && go run . -root $(ROOT)

coverage: $(OUT) test gates ## Run every suite, then generate the 8 gate reports + consolidated evidence

$(OUT):
	mkdir -p $(OUT)

clean: ## Remove generated reports and build outputs
	rm -rf coverage/out frontend/coverage frontend/dist e2e/coverage e2e/test-results e2e/playwright-report backend/bin
