# Architecture (POC)

## Modular monolith

One Go process, three modules with their own internal interfaces. No queues, RPC or service clients in the POC.

| Module | Owns (tables) | Public interface | Depends on |
|---|---|---|---|
| `catalog` (Test Catalog) | `test_cases`, `test_steps` | `catalog.Service` | — |
| `execution` (TestRun/Execution) | `test_runs`, `test_run_expected_cases`, `test_results` | `execution.Service` | `catalog` only through `TestCaseChecker` (404 on history) |
| `ingestion` | none | `ingestion.Service` | `catalog` (`ExpectedUniverse`, `Statuses`), `execution` (`RecordRun`, `Diagnostics`) |

- A module never reads another module's tables and there are **no cross-module foreign keys**: execution stores
  TC-IDs by value, keeping a future extraction possible.
- Each module has a `Repository` port and a `postgres` adapter over its own `sqlc` package generated from its own
  migration file only (no shared global schema).
- `internal/app` composes modules; `app.Services` exposes the application layer so REST today and MCP later share
  the same use cases.

## Key invariants and where they are enforced

| Invariant | Enforcement |
|---|---|
| TC-ID numeric, server-assigned, immutable, never reused | `GENERATED ALWAYS AS IDENTITY (NO CYCLE)`; trigger forbids `DELETE` and id changes; API rejects unknown fields (e.g. `id`) |
| Content does not version identity | `PATCH` only edits content columns; results reference the TC-ID only |
| Idempotent TestRun per `{provider}:{run_id}:{run_attempt}` | `UNIQUE(external_run_id)` + `INSERT … ON CONFLICT DO NOTHING` in one transaction with snapshot and results |
| Expected-universe snapshot is immutable | written once at run creation; trigger forbids `UPDATE`/`DELETE` |
| `untested` is never persisted | `CHECK` on `test_results.status`; derived in `execution.ComputeSummary` |
| `testCaseId` only for valid or deprecated correlations (deprecated results stay in history) | `CHECK ((correlation IN ('valid','deprecated')) = (test_case_id IS NOT NULL))` |

## REST conventions (see `api/openapi.yaml`)

- Errors: `application/problem+json` with a stable `code` (`validation_error`, `not_found`, `invalid_junit`, …) and
  optional field `errors`. Internal errors never leak details.
- Pagination: `page` (1-based) and `pageSize` (1..100, default 20); responses carry `items`, `page`, `pageSize`,
  `totalItems`, `totalPages`.
- Ingestion: `POST /api/v1/ingestion/junit?provider=&runId=&runAttempt=[&pipeline=&branch=&commit=]` with the
  JUnit XML as `application/xml` body (one request per complete report). `201` creates the run, `200` is an idempotent
  replay (nothing re-processed).

## JUnit → TC-ID extraction

1. `<properties><property name="tc-id" value="153"/></properties>` inside the `<testcase>` (value `153` or `TC-153`).
2. Otherwise the `TC-<id>` pattern in the `name` attribute.

Outcomes: `valid`, `missing` (none declared; only uppercase `TC-` counts), `malformed` (not a single positive
integer — leading zeros are accepted, `TC-0153` = `TC-153` — or several different ids in one `<testcase>`),
`unknown` (no such TC), `deprecated`. Problems with individual testcases never stop the batch: a testcase without a
name is discarded, an invalid `time` keeps the result with an unknown (`null`) duration; both are stored as run parse
errors (`GET /api/v1/test-runs/{id}/parse-errors`). An unreadable document is a `400 invalid_junit`.
