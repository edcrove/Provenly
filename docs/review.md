# POC review (2026-09-28)

A full review of the POC against the 21 POC Trello cards. Each acceptance criterion (AC) must map to at least one
explicit test, even where code coverage is already 100%. Test ids refer to `coverage/inventories/*.yaml`; unit
tests are named by function.

Layers: **U** = unit, **I** = integration (backend: testcontainers; frontend: Testing Library + MSW), **C** = contract,
**E** = E2E (Playwright).

## Findings fixed in this review

| # | Finding | Fix | Test |
|---|---|---|---|
| 1 | Suite timestamps with milliseconds or an offset were ignored, so Playwright reports (`toISOString()`, e.g. `…T10:00:00.000Z`) had no `startedAt`. Playwright is the first native reporter (decision 15). | The parser accepts RFC 3339 (fractions, `Z`, offsets) plus the zone-less form, normalized to UTC. | U `TestParseSuiteTimestampFormats`; E BE-E2E-004 (fixtures now send Playwright timestamps) |
| 2 | `formatDuration` could show `60.0 s` or `1m 60s` because of rounding. | Seconds are rounded before being split into minutes. | U `formats durations` |
| 3 | Singular/plural text: "1 results", "1 items", "1 result(s) point to…". | New `plural()` helper, used in the run list, pagination, execution counts and outside-universe message. | U `pluralizes counts`; I FE-INT-008, FE-INT-010 |
| 4 | Validation errors appeared only as technical text ("request validation failed (title: is required)"). | Field errors are also shown under the field, with `aria-invalid` and `aria-describedby`. | I FE-INT-003; screenshot 22 |
| 5 | A missing resource showed "Something went wrong". | 404 errors are titled "Not found". | I FE-INT-012; screenshots 23–24 |
| 6 | Screenshots 13–14 were cut off (captured before the filtered table rendered). | The capture waits for the filtered rows. | screenshots 13–14 |

## Acceptance criteria that lacked an explicit test (added)

| Card | AC | Added test |
|---|---|---|
| TestRun Data Model & Lifecycle | A replay does not recompute the snapshot (only unit-tested with a fake) | I BE-INT-008: the catalog changes before the replay and the snapshot stays at 1 |
| TestRun Data Model & Lifecycle | Supports the created/running/completed/failed/cancelled lifecycle | I BE-INT-011: the DB accepts all 5 statuses and rejects others |
| Test Steps Management | Editing steps does not change the identity or the historical results | I BE-INT-021 (new): edit, reorder and delete steps, then compare the history |
| End-to-End POC Demo Flow | Editing TC-153's content does not change how its earlier results read (UI) | E FE-E2E-002 step 5: edit the TC and the history is unchanged |

## Traceability matrix

### Foundation

| Card | AC | Tests |
|---|---|---|
| Repository and Project Initialization | Monorepo with `backend/` and `frontend/`, README, conventions | Structure; `make lint` (gofmt, golangci-lint, eslint, prettier) in CI |
| Backend Scaffolding | Runnable app, net/http routing, config by environment | U `TestLoadDefaults/Overrides/Errors`, `TestServe*`, `TestHandlerWiring`, `TestRunWiresTheCLI`; I BE-INT-015 |
| | Health check | U `TestHandlerWiring`; C `TestSystem`; E BE-E2E-001 |
| | REST + OpenAPI; domain independent from the UI; modules without cross-module table access | C (every operation validated against `api/openapi.yaml`); separate per-module sqlc schemas (`sqlc.yaml`), with no cross-module FKs |
| OpenAPI Contract Specification | The contract is the inventory of public operations | backend-contract gate: op × status derived from the spec (72 variants) |
| | Consistent error model and paging convention | U `TestWriteErrorMapping`, `TestParsePage`, `TestPage`; C every problem+json variant |
| | Requests/responses validated against the contract in CI | C kin-openapi (backend), frontend contract (MSW handlers + generated client validated against the spec) |
| Database Setup | Postgres, sqlc without a global schema, reproducible goose migrations | I BE-INT-001, BE-INT-Q:* (every query) |
| Frontend Scaffolding | React 19 + shadcn, routing and layout | I FE-INT-001, FE-INT-013 |
| | Client generated with openapi-typescript, with no handwritten types | `make check-generated` in CI; U `createApiClient` |
| | Vitest + Testing Library + MSW; istanbul build | frontend-unit/integration gates; frontend-e2e evidence (istanbul) |
| Local Development Setup | Compose, `.env.example`, scripts, reproducible from README | Verified from a fresh clone; `.tool-versions`. Superseded 2026-09-30 by the Docker environments (DEC-54): `docker compose up`, `make up/dev ENV=…`, CI docker smoke job |
| POC Testing Pyramid Foundation | 4 layers × 2 sides with real tests | 8 gates at 100% + 2 consolidated gates |
| CI Pipeline | Push/PR; build, lint, vet, typecheck, generated-code drift; all layers | `.github/workflows/ci.yml` (10 jobs) |
| | Gates publish actual/target/gap; failures turn CI red; exceptions justified | `coverage/out/coverage-report.md`; U covgate `TestEvaluateKeepsExceptionsVisible`, `TestGateSelection` |
| Coverage Measurement & Exceptions Framework | Versioned targets and exceptions with every field; invalid or expired ones fail | U covgate `TestExceptionValidation` (fields, category, expired, stale); `coverage/exceptions.yaml` |
| | One command produces 8 reports + consolidated | `make coverage` |

### Test management

| Card | AC | Tests |
|---|---|---|
| Test Case Data Model + Numeric ID | Unique, immutable numeric ID shown as TC-\<id\> | U `TestFormatKey`; I BE-INT-002, BE-INT-003 |
| | Never reused, even after deprecation | I BE-INT-002 |
| | Fields and timestamps; active/deprecated status | I BE-INT-004; C `TestTestCases` |
| | Related to steps and results without an AutomationTest entity | I BE-INT-006, BE-INT-012 |
| Test Case CRUD API | Create with a server-assigned TC-ID; a client id is rejected | U `TestCreateAssignsIDAndTrims`, `TestDecodeJSON` (unknown field); E BE-E2E-002 |
| | Get and paginated list | U `TestList`; I BE-INT-004; C |
| | Edit title/description/expectedResult/automated; TC-ID unchanged | U `TestUpdateKeepsIdentity`, `TestUpdateValidation`; I BE-INT-004 |
| | Deprecate without losing history; leaves the universe | U `TestDeprecate`; I BE-INT-005, BE-INT-010; E BE-E2E-006 |
| | Editing creates no version and does not change historical results | I BE-INT-021; E BE-E2E-006, FE-E2E-002 |
| | Errors follow the contract (+ reactivate, decision 10) | C `TestTestCases`; U `TestReactivate`; E BE-E2E-002, FE-E2E-001 |
| Test Case UI | Paginated list with TC-ID, title, status and automated | I FE-INT-002; screenshots 07, 10, 26 |
| | Create (TC-ID from the backend), edit, deprecate | I FE-INT-003/004/005; E FE-E2E-001 |
| | Detail with fields and steps | I FE-INT-004/006; screenshot 04 |
| Test Step Data Model | Persisted entity with action, expected result and order, belonging to a TC | I BE-INT-006, BE-INT-007 |
| Test Steps Management | Add/edit/delete/reorder through the API and the UI | U `TestStepsLifecycle`; C `TestTestSteps`; I BE-INT-006, FE-INT-006; E BE-E2E-003, FE-E2E-001 |
| | A TC is valid without steps | E BE-E2E-003; I FE-INT-006 (empty state) |
| | Editing steps keeps the identity and history | I BE-INT-021 |

### Automation and TestOps

| Card | AC | Tests |
|---|---|---|
| JUnit Result Parser | NormalizedTestResult independent of the format; fields | U `TestParseStatusesAndFields` |
| | Property `tc-id` first, `TC-<id>` in the name as fallback | U `TestExtractRef` (a property wins over the name) |
| | passed/failed/error/skipped | U `TestParseStatusesAndFields` |
| | Invalid cases are reported without stopping parsing | U `TestParseInvalidCasesDoNotStopParsing`; I BE-INT-018 |
| | Decisions 4/5: leading zeros, uppercase, several ids, data providers, severity, 0 ms | U `TestExtractRef`, `TestParameterizedTestsDeclareOneIDPerInvocation`; E BE-E2E-004 |
| TestRun Data Model & Lifecycle | Unique externalRunId `{provider}:{run_id}:{run_attempt}` | U `TestExternalRunID`; I BE-INT-011 (DB format) |
| | Pipeline/branch/commit/timestamps | I BE-INT-012; U `TestParseSuiteTimestampFormats` |
| | Immutable expected-universe snapshot | I BE-INT-010 |
| | Lifecycle created/running/completed/failed/cancelled | I BE-INT-011, BE-INT-019 |
| | A replay creates no duplicate and does not recompute the snapshot | U `TestRecordRunIsIdempotentPerAttempt`; I BE-INT-008 |
| | A rerun (new attempt) creates a new run and keeps history | I BE-INT-009; E BE-E2E-005, FE-E2E-003 |
| Test Result Persistence | Linked to run and TC; no AutomationTest | I BE-INT-012 |
| | Statuses; untested is never persisted | I BE-INT-011 |
| | testName, duration, error and origin metadata | I BE-INT-012, BE-INT-014, BE-INT-018 |
| | requestedTestCaseId + correlation; no TC is created automatically | U `TestIngestCorrelatesEveryResult`; E BE-E2E-004 |
| | Several results per TC are kept | E BE-E2E-004 (4 results for one TC) |
| | Read APIs (runs, run results, TC history), paginated | I BE-INT-013/014; C `TestRunsAndIngestion` |
| CI/CD Result Ingestion API | REST endpoint in the contract; one batch per report | C `TestRunsAndIngestion`; U `TestIngestHandlerCreatedAndReplay` |
| | Metadata → externalRunId; validation | U `TestValidateMeta`, `TestIngestHandlerErrors` |
| | Diagnostics missing/malformed/unknown/deprecated in the response | U `TestIngestCorrelatesEveryResult`; E BE-E2E-004 |
| | One invalid case does not block the batch | I BE-INT-018; E BE-E2E-004 |
| | Idempotent (+ warnings, decision 6) and final status (decision 7) | I BE-INT-008/019; E BE-E2E-005 |

### Reporting

| Card | AC | Tests |
|---|---|---|
| Basic TestRun Summary | Universe = snapshot; untested derived | U `TestComputeSummary`, `TestSummaryUsesSnapshot`; I BE-INT-010 |
| | Precedence failed > error > skipped > passed | U `TestAggregatePrecedence`; E BE-E2E-004 (Chrome PASS + Firefox FAIL = failed) |
| | Counts and the 3 percentages; 0 denominator | U `TestComputeSummary`, `TestComputeSummaryRoundingAndEmpty`; I FE-INT-009 |
| | Invalid TC-IDs outside the universe; outside-universe results | U `TestComputeSummary`; I BE-INT-017 |
| | Later catalog changes do not change the summary | I BE-INT-010; E BE-E2E-006, FE-E2E-004 |
| | Exposed through the API | C `TestRunsAndIngestion` |
| TestRun Detail UI | Paginated list and detail | I FE-INT-008/009; screenshots 11–17 |
| | Metadata, summary, results with TC-ID links, diagnostics, status filter | I FE-INT-009/010/011/015/016; E FE-E2E-002 |
| | TC history (PASS/FAIL per run) | I FE-INT-007; E FE-E2E-002; screenshots 09, 20 |
| | Current definition vs observed metadata | I FE-INT-004; E FE-E2E-002 |

### Validation

| Card | AC | Tests |
|---|---|---|
| End-to-End POC Demo Flow | TC-153 declared through a property/name, CI report, run + snapshot + result | E FE-E2E-002, BE-E2E-004 |
| | Replay without duplicates; a rerun creates a new run | E BE-E2E-005, FE-E2E-003 |
| | Summary with the 3 percentages; PASS+FAIL = failed with both results visible | E FE-E2E-002, BE-E2E-004 |
| | Creating, deprecating or editing TCs does not change an existing summary | E BE-E2E-006, FE-E2E-004 |
| | Editing TC-153 does not change how its results read; a later run shows history | E BE-E2E-006, FE-E2E-002 (steps 4 and 5) |
| | 8 gates at 100%, CI green, setup reproducible | `make coverage`; CI; fresh clone verified |

## Still open (not POC scope, or pending a decision)

- **Snapshot amendment** (decision 2, part 2): Proposed in the Decision Register; not implemented because it
  changes the "immutable snapshot" invariant.
- **Suite-level `tc-id` property**, or one testcase name listing every id: not supported (documented in decision 4).
- **Core MVP**: streaming/reconciliation, OpenTelemetry, auth/login, shadcn regenerated with Radix, major-version
  upgrades (TypeScript 7, MSW 3, Playwright 1.63, Node 24).
