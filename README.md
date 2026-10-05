# Provenly

Open-source QA / test-management platform (formerly *OpenTestHub*). This repository holds the **POC**: a
vertical slice where a Test Case gets a permanent numeric id (`TC-<id>`), an automated test declares that id, CI
sends a JUnit XML report, Provenly ingests it into an idempotent TestRun and a UI/API shows results, a
snapshot-based summary and per-test-case history. Everything runs locally; sign in with the demo administrator (`admin` / `provenly-demo`, see `envs/demo.env`).

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

- To **run** Provenly: only Docker (Compose v2.24+).
- To **develop and test** without containers: Go **1.27.1+**, Node.js **22+** (see `.tool-versions`) and Docker,
  which the Integration and Contract suites also need (testcontainers-go). Optional: [`sqlc`](https://docs.sqlc.dev)
  1.31 to regenerate queries.

## Run it with Docker

```bash
git clone <this repo> provenly && cd provenly
docker compose up -d --build  # demo environment: UI http://localhost:3000 · API http://localhost:8080
```

That single command builds the images (`--build` rebuilds them after a `git pull`; without it Compose reuses
images built from older code) and starts PostgreSQL, restores the demo snapshot into the empty database,
applies migrations, then starts the API and the UI (nginx). Data persists across `docker compose down`/`up`.

Three isolated environments can run side by side, each with its own data (details:
[`docs/environments.md`](docs/environments.md)):

| Environment | Purpose | UI / API / DB ports | Starts with |
|---|---|---|---|
| `demo` | showing Provenly | 3000 / 8080 / 5432 | the demo snapshot (`make demo-reset` restores it) |
| `qa` | manual testing | 3100 / 8180 / 5433 | the demo snapshot (any seed, reset freely) |
| `prod` | real data of a project | 3200 / 8280 / 5434 | empty; resets need `CONFIRM=prod` and dump first |

```bash
make up ENV=qa                         # build + start an environment (demo by default)
make dev ENV=qa                        # same data, hot reload (api: air, web: Vite HMR)
make down ENV=qa                       # stop, keeping the data
make db-dump ENV=prod                  # backups/prod-<timestamp>.sql
make seed-snapshot FROM=qa NAME=sprint # take qa's data as a new seed: seeds/sprint.sql
```

Automated suites never touch these environments: Integration and Contract use testcontainers, and E2E /
screenshots use an ephemeral in-memory database (`docker-compose.e2e.yml`) destroyed after each run.

### Without Docker for the services (contributors)

```bash
make setup                    # go mod download + npm ci (frontend, e2e)
cp .env.example .env.local    # optional: local settings (database URL, log level, API port...)
make infra ENV=qa             # only qa's PostgreSQL (seeded + migrated) on :5433
make dev-backend              # API on http://localhost:8080  (terminal 1; stop the demo env first)
make dev-frontend             # UI  on http://localhost:5173  (terminal 2)
```

Screenshots of every UI flow: [`docs/screenshots`](docs/screenshots/README.md) (regenerate with `make screenshots`).

### Try the POC flow with curl

```bash
# 0. Sign in (the demo administrator of envs/demo.env); the session cookie goes to cookies.txt
curl -s -c cookies.txt -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' -d '{"username":"admin","password":"provenly-demo"}'

# 1. Create an automated test case; the TC-ID is assigned by Provenly
curl -s -b cookies.txt -X POST localhost:8080/api/v1/test-cases \
  -H 'Content-Type: application/json' \
  -d '{"title":"User can log in","expectedResult":"Dashboard is shown","automated":true}'
# => {"id":8,"key":"TC-8",...}   (use the returned id below; the demo data already holds TC-1..TC-7)

# 2. CI sends the JUnit report of run 42, attempt 1 (two results for the same TC: Chrome PASS, Firefox FAIL)
cat > report.xml <<'XML'
<testsuites><testsuite name="auth" timestamp="2026-09-28T10:00:00">
  <testcase name="login chrome"><properties><property name="tc-id" value="8"/></properties></testcase>
  <testcase name="login firefox TC-8"><failure message="button not found"/></testcase>
  <testcase name="test without id"/>
</testsuite></testsuites>
XML
curl -s -X POST -H 'Content-Type: application/xml' --data-binary @report.xml \
  'localhost:8080/api/v1/ingestion/junit?provider=github&runId=42&runAttempt=1&branch=main&commit=abc123'
# => 201, created=true, testRun.id=6, diagnostics=[missing TC-ID]. Re-sending the same attempt returns 200.

# 3. Summary (snapshot universe, aggregated failed > error > skipped > passed, 3 percentages) and history
curl -s -b cookies.txt localhost:8080/api/v1/test-runs/6/summary
curl -s -b cookies.txt localhost:8080/api/v1/test-cases/8/results
```

Then open http://localhost:3000 → *Test Runs* → run #6, or *Test Cases* → TC-8 for its history.

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
make coverage               # runs every suite (ephemeral databases), then prints the 8 gate reports + consolidated evidence
make gates                  # re-evaluate gates from already collected evidence (coverage/out/coverage-report.md)
```

CI (GitHub Actions, `.github/workflows/ci.yml`) runs build, lint, vet, typecheck, generated-code drift checks and every
gate on each push / pull request; the `coverage-report` job publishes all 8 gates in the run summary.

## Conventions

See [`CONTRIBUTING.md`](CONTRIBUTING.md) (gofmt/golangci-lint, eslint/prettier, generated code, how a feature updates
every test layer, coverage exceptions).

## License

[Apache License 2.0](LICENSE). See [`NOTICE`](NOTICE).
