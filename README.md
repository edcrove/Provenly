# Provenly

Open-source QA / test-management platform (formerly *OpenTestHub*). This repository holds the **POC**: a
vertical slice where a Test Case gets a permanent numeric id (`TC-<id>`), an automated test declares that id, CI
sends a JUnit XML report, Provenly ingests it into an idempotent TestRun and a UI/API shows results, a
snapshot-based summary and per-test-case history. Everything runs locally, without authentication.

```
TC-153 created ──► automated test declares TC-153 ──► CI posts JUnit XML ──► Provenly
                     <property name="tc-id" value="153"/>                     ├─ extracts / validates TC-ID
                     (fallback: "TC-153" in the test name)                    ├─ creates or reuses TestRun {provider}:{run_id}:{run_attempt}
                                                                              ├─ persists TestResults (+ diagnostics)
                                                                              └─ summary · run detail · history (API + UI)
```

## Repository layout (monorepo, independently deployable services)

| Path | What |
|---|---|
| `api/openapi.yaml` | **The REST contract** (OpenAPI 3.0). Source of truth for the backend Contract gate and the generated frontend client. |
| `backend/` | Go service (`net/http`, PostgreSQL via `sqlc` + `pgx`, `goose` migrations). Modular monolith: `catalog`, `execution`, `ingestion`. |
| `frontend/` | React 19 + Vite + shadcn/ui. API client generated with `openapi-typescript` (never hand-written). |
| `e2e/` | Playwright journeys shared by the backend and frontend E2E gates. |
| `coverage/` | Gate inventories and the versioned exception registry. |
| `tools/covgate/` | Evaluates the 8 coverage gates and publishes actual / target / gap. |
| `docs/` | Architecture, testing strategy and implementation decisions. |

## Prerequisites

- Go **1.26+**, Node.js **22+**, Docker (Compose v2). Docker is also required by the Integration and Contract
  suites (testcontainers-go).
- Optional for regeneration only: [`sqlc`](https://docs.sqlc.dev) 1.30.

## Run it locally from scratch

```bash
git clone <this repo> provenly && cd provenly
cp .env.example .env          # no secrets needed
make setup                    # go mod download + npm ci (frontend, e2e)
make up                       # PostgreSQL 16 via docker compose (also creates provenly_e2e)
make migrate                  # goose migrations
make dev-backend              # API on http://localhost:8080  (terminal 1)
make dev-frontend             # UI  on http://localhost:5173  (terminal 2)
```

### Try the POC flow with curl

```bash
# 1. Create an automated test case; the TC-ID is assigned by Provenly
curl -s -X POST localhost:8080/api/v1/test-cases \
  -H 'Content-Type: application/json' \
  -d '{"title":"User can log in","expectedResult":"Dashboard is shown","automated":true}'
# => {"id":1,"key":"TC-1",...}   (use the returned id below; "153" in the docs is illustrative)

# 2. CI sends the JUnit report of run 42, attempt 1 (two results for the same TC: Chrome PASS, Firefox FAIL)
cat > report.xml <<'XML'
<testsuites><testsuite name="auth" timestamp="2026-09-28T10:00:00">
  <testcase name="login chrome"><properties><property name="tc-id" value="1"/></properties></testcase>
  <testcase name="login firefox TC-1"><failure message="button not found"/></testcase>
  <testcase name="test without id"/>
</testsuite></testsuites>
XML
curl -s -X POST -H 'Content-Type: application/xml' --data-binary @report.xml \
  'localhost:8080/api/v1/ingestion/junit?provider=github&runId=42&runAttempt=1&branch=main&commit=abc123'
# => 201, created=true, diagnostics=[missing TC-ID]. Re-sending the same attempt returns 200, created=false.

# 3. Summary (snapshot universe, aggregated failed > error > skipped > passed, 3 percentages) and history
curl -s localhost:8080/api/v1/test-runs/1/summary
curl -s localhost:8080/api/v1/test-cases/1/results
```

Then open http://localhost:5173 → *Test Runs* → run #1, or *Test Cases* → TC-1 for its history.

## Testing: 8 independent gates

Backend and frontend each have Unit, Integration, Contract and E2E suites. **Each gate must reach 100% of its own
denominator** — a consolidated number never replaces a gate. Details: [`docs/testing-strategy.md`](docs/testing-strategy.md).

| Gate | Tooling | Denominator (100% required) | Command |
|---|---|---|---|
| backend-unit | `testing` + testify, `go test -cover` | Go statements (branches: complementary inventory) | `make test-backend-unit` |
| backend-integration | testcontainers-go (real Postgres) | every sqlc query + reviewed behaviors | `make test-backend-integration` |
| backend-contract | httpexpect + kin-openapi | every OpenAPI operation × response status | `make test-backend-contract` |
| backend-e2e | Playwright (API) on `go build -cover` binary | reviewed API journeys | `make test-e2e` |
| frontend-unit | Vitest + @vitest/coverage-v8 | statements + branches | `make test-frontend-unit` |
| frontend-integration | Vitest + Testing Library + MSW | reviewed behaviors + every UI surface inventoried | `make test-frontend-integration` |
| frontend-contract | generated client + MSW validated against the spec | consumed operations × response status | `make test-frontend-contract` |
| frontend-e2e | Playwright (UI) on istanbul-instrumented bundle | reviewed UI journeys | `make test-e2e` |

On top of the 8 gates, two **consolidated gates** require 100% of all code to be executed by some layer, on the
coverage of all layers merged (so anything excepted from a Unit gate must be covered elsewhere; lines no layer can
execute are listed exceptions). The per-file consolidated report is
`coverage/out/consolidated-coverage.md` (backend HTML: `coverage/out/backend-consolidated.html`).

```bash
make up && make coverage    # runs every suite, then prints the 8 gate reports + consolidated code-coverage evidence
make gates                  # re-evaluate gates from already collected evidence (coverage/out/coverage-report.md)
```

CI (GitHub Actions, `.github/workflows/ci.yml`) runs build, lint, vet, typecheck, generated-code drift checks and every
gate on each push / pull request; the `coverage-report` job publishes all 8 gates in the run summary.

## Conventions

See [`CONTRIBUTING.md`](CONTRIBUTING.md) (gofmt/golangci-lint, eslint/prettier, generated code, how a feature updates
every test layer, coverage exceptions).
