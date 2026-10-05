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
| Every test case and run belongs to a project; the displayed key is `<projectKey>-<number>` (e.g. `CHK-12`), numbers per project, contiguous, never reused | `projects.next_number` incremented in the same statement that inserts the test case (`CreateTestCase` CTE, row lock serializes creators); `UNIQUE(project_id, number)`; triggers forbid deleting projects, changing a key or lowering the counter, and changing a test case's project or number (migration 00012). Existing test cases became `TC-<id>` in the default project `TC` |
| Runs are per project | `test_runs.project_id NOT NULL`, `UNIQUE(project_id, external_run_id)` (the same CI run id in two projects is two runs); the project is part of the run's protected identity (migration 00013) |
| Test case content rules hold in the database too | `CHECK`s: non-blank title (1..200), description and expected result at most 10000 characters, `deprecated_at` set exactly while deprecated, `updated_at >= created_at` (migration 00011) |
| Content does not version identity | `PATCH` only edits content columns; results reference the TC-ID only |
| Snapshot and correlations agree | the expected universe and the statuses of the referenced TC-IDs come from one catalog statement (`ListIngestionView`) |
| Idempotent TestRun per project and `{provider}:{run_id}:{run_attempt}` | `UNIQUE(project_id, external_run_id)` + `INSERT … ON CONFLICT DO NOTHING` in one transaction with snapshot and results |
| Expected-universe snapshot is immutable | written once at run creation; triggers forbid `UPDATE`/`DELETE` and any `INSERT` outside the run's creating transaction (migration 00009) |
| A run's identity and history are permanent | trigger forbids deleting runs and changing `external_run_id`, provider, run id, attempt, report digest or `created_at` (status and timestamps stay open for the live lifecycle); `CHECK started_at <= completed_at`; a suite timestamp later than the ingestion leaves `startedAt` unknown with a warning |
| Steps belong to one test case, ordered 1..n without gaps, at most 100 | FK to `test_cases`; `UNIQUE(test_case_id, position) DEFERRABLE` + `CHECK (position >= 1)`; the service locks the test case row to shift/renumber and to enforce the 100 limit; trigger forbids moving a step to another test case |
| Step text: action 1..2000 non-blank characters, expected result ≤ 2000 | service validation (400) backed by `CHECK`s (`test_steps_action_not_blank`, `test_steps_expected_result_length`, migration 00008) |
| Ingested results and parse errors are the source of truth | written only by the transaction that creates their run; triggers forbid later `INSERT`, any `UPDATE` and `DELETE` (migration 00010) |
| `untested` is never persisted | `CHECK` on `test_results.status`; derived in `execution.ComputeSummary` |
| `testCaseId` only for valid or deprecated correlations (deprecated results stay in history) | `CHECK ((correlation IN ('valid','deprecated')) = (test_case_id IS NOT NULL))` |

## REST conventions (see `api/openapi.yaml`)

- Errors: `application/problem+json` with a stable `code` (`validation_error`, `not_found`, `invalid_junit`, …) and
  optional field `errors`. Internal errors never leak details.
- Pagination: `page` (1-based) and `pageSize` (1..100, default 20); responses carry `items`, `page`, `pageSize`,
  `totalItems`, `totalPages`.
- Query parameters: unknown ones are ignored; every known parameter is applied and validated (an invalid value of a
  known parameter is a `400` even when unknown ones are present). A known parameter present but empty (`status=`) is
  invalid; a repeated one uses its first value. A page whose offset does not fit the database is a `400`.
- Request bodies: JSON operations require `Content-Type: application/json` (`415 unsupported_media_type` otherwise);
  duplicate keys keep the last value. Text fields (JSON or query) must be valid UTF-8 without NUL characters (`400`
  otherwise), since PostgreSQL cannot store them. Every validation error has the detail `request validation failed` and lists
  the offending fields in `errors`.
- Ingestion: `POST /api/v1/ingestion/junit?provider=&runId=&runAttempt=[&pipeline=&branch=&commit=]` with the
  JUnit XML as `application/xml` body (one request per complete report; `text/xml` and `*+xml` too; a Content-Type
  `charset` overrides the XML declaration; compressed bodies are a 415; every parameter error is listed at once). `201` creates the run, `200` is an idempotent
  replay (nothing re-processed).

## JUnit → TC-ID extraction

A run belongs to the project named by `?project=<KEY>` (default `TC`); numbers are resolved inside that project.

1. `<properties><property name="tc-id" value="153"/></properties>` inside the `<testcase>` (value `153`, `TC-153`,
   or any project key such as `CHK-12`; a bare number is the run's project).
2. Otherwise the `<runKey>-<n>` pattern in the `name` attribute (only the run's own key, upper-case).

Outcomes: `valid`, `missing` (none declared; only uppercase `TC-` counts), `wrong_project` (a property with another
project's key: never correlated across projects), `malformed` (not a single positive
integer — leading zeros are accepted, `TC-0153` = `TC-153` — or several different ids in one `<testcase>`),
`unknown` (no such TC), `deprecated`. Problems with individual testcases never stop the batch: a testcase without a
name is discarded, an invalid `time` keeps the result with an unknown (`null`) duration; both are stored as run parse
errors (`GET /api/v1/test-runs/{id}/parse-errors`). An unreadable document — not well-formed, an unsupported
encoding (UTF-8, US-ASCII, ISO-8859-1, windows-1252 and UTF-16 are read) or content after the root element — is a
`400 invalid_junit`. `time` accepts locale decimal commas and digit grouping (implementation decision #5).
