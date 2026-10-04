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

## Findings fixed during card validation (2026-10-01)

| # | Card | Finding | Fix | Test |
|---|---|---|---|---|
| 7 | Test Case CRUD API | A huge `page` overflowed the SQL offset (500 on every list). | 400 when the offset does not fit; JSON bodies require `application/json` (415); empty known parameters are invalid. | U `TestQueryEdgeCases`, fuzz `FuzzParsePage`/`FuzzPathID`/`FuzzParse`; C `TestRobustness` |
| 8 | Test Case UI | A fast double click on "Create test case" created two test cases. | Mutations ignore calls while one is in flight. | I FE-INT-018 |
| 9 | Test Case UI | `?page=999` showed "No test cases yet." with items present. | A page past the end moves to the last page (history replace). | I FE-INT-018 |
| 10 | Test Case UI | At 375 px the list header overflowed and TC-IDs wrapped. | The header wraps; TC-IDs do not break. | Manual (Chromium 375 px) |
| 11 | Test Case UI | Invalid ids (`/test-cases/abc`) called the API and read "Something went wrong"; error/loading pages kept the generic tab title; a filter without matches read "No test cases yet." | Invalid ids render *Page not found* without a request; tab titles "Loading…", "Not found", "Error"; "No deprecated test cases." | I FE-INT-018 |
| 12 | Test Case UI | At 375 px a detail page with a history table grew to 780 px wide (page-level horizontal scroll). | Cards shrink below their content (`min-w-0`); wide tables scroll inside their card. | Screenshots 31–34 (`flows.spec.ts` asserts no page overflow at 375 px) |
| 13 | TestRun Data Model & Lifecycle, Basic TestRun Summary, TestRun Detail UI | A run's `status` mixed how the CI execution ended with test outcomes: a run with failing tests read grey `completed`, a broken pipeline read red `failed`; `created`/`running` were in the API but never produced; the list had no verdict. | Decisions E1–E5 (2026-10-02): `executionStatus` = completed / interrupted / cancelled (migration 00007 renames `failed`); derived `outcome` with `verdict` (no_tests > failed > incomplete > passed), counts and `passRate` (% of executed that passed) on every run; list and detail show verdict, pass rate and breakdown, execution only when not completed. | U `TestOutcomeVerdict`, `TestRunsCarryTheirOutcome`; I BE-INT-011, FE-INT-008/009/016; C `TestRunsAndIngestion`; E FE-E2E-007, BE-E2E-004 |
| 14 | Test Step Data Model | A NUL character (`\u0000`) in any text field (step action/expected result, test case title/description/expected result, ingestion `pipeline`/`branch`/`commit`) or invalid UTF-8 in a query parameter reached PostgreSQL and returned 500. | `Validator.CheckText`: such text is a 400 `validation_error` on every text field. | U `TestCheckTextRejectsUnstorableText`, catalog/ingestion validation tests, fuzz `FuzzCreateText`; C `TestRobustness` |
| 15 | Test Step Data Model | The database accepted what only the service rejected: a blank action (`'   '`), an expected result over 2000 characters, and moving a step to another test case; the identity trigger messages read `TC-1s` (`RAISE` format typo). | Migration 00008: `CHECK`s for non-blank action and expected result ≤ 2000, trigger forbidding a step's `test_case_id` change, trigger messages fixed (`TC-1`). | I BE-INT-003 (exact messages), BE-INT-023 |
| 16 | Test Steps Management | A long unbroken step action/expected result (also a test case title or description) widened the page to 2300–5100 px, at 375 px and on desktop; the Add step form emptied itself before the API answered, so a rejected step (blank action, 101st step) lost what was typed; the error banner showed the first failing action of the session (e.g. an old "too many steps") instead of the latest one, and a failed action left a stale list on screen. | Text wraps anywhere when it would overflow (`overflow-wrap: anywhere` on the body, tables keep normal wrapping and scroll in their card); the form clears only on success; only the latest action's error is shown (reset when a new action starts); step mutations refetch the list on failure too. | I FE-INT-019; E FE-E2E-008 |
| 17 | JUnit Result Parser | A `time` too large for milliseconds (`1e300`, `9.3e15` s) overflowed and the whole ingestion returned 500; a decimal comma (`0,123`, Surefire in es/de locales) read as 123 s and `1.234,5` as 1.2 s; hex `0x1p3` was accepted as 8 s; a report declaring ISO-8859-1, US-ASCII or UTF-16 was rejected entirely; a second root element (two concatenated reports) was silently dropped. | `time` is a plain decimal with locale separators undone and capped at 2^53−1 ms (larger = invalid, kept without duration); ISO-8859-1, windows-1252, US-ASCII and UTF-16 are transcoded; other encodings and content after the root are `400 invalid_junit`. Decision #5 addendum. | U `TestParseDurationFormats`, `TestParseDeclaredEncodings`, `TestParseRejectsContentAfterTheRoot`, `FuzzParse` (duration bound); C `TestRobustness` |
| 18 | TestRun Data Model & Lifecycle | The database let a TC-ID be inserted into an existing run's snapshot, a run's `external_run_id`/provider/run id be rewritten and an empty run be deleted (only the service prevented it); a replay with another pipeline/branch/commit was silently ignored (only a different status or report warned); a suite timestamp in the future (`2030-…`, or a local time without zone ahead of UTC) stored `startedAt` after `completedAt`. | Migration 00009: triggers forbid run deletion, identity changes and snapshot inserts after the creating transaction; `CHECK started_at <= completed_at`. Replays warn per differing pipeline/branch/commit; a future suite timestamp leaves `startedAt` unknown with a warning. | U `TestIngestWarnsWhenAReplayCarriesOtherMetadata`, `TestIngestWarnsWhenTheSuiteTimestampIsLaterThanIngestion`, `TestRecordRunLeavesAFutureStartUnknown`; I BE-INT-024 |
| 19 | Test Result Persistence | "The ingested final result is the source of truth" was only kept by the service: the database accepted inserting a result into an existing run, changing a result's status (a failed run then read as passed) and deleting results or parse errors. | Migration 00010: results and parse errors are written only by the transaction that creates their run and are never updated or deleted. | I BE-INT-025 (BE-INT-011 now probes invalid rows inside a run's creating transaction) |
| 20 | Test Result Persistence (sweep) | A `<testcase>` with two `<failure>` kept only the last one and with `<failure>` + `<error>` the error was dropped; a `tc-id` property on a `<testsuite>` and a `<testcase>` nested in another were ignored silently; suite timestamps with `+0530`, a space instead of `T` or an impossible date left `startedAt` empty without a word; a non-ASCII TC-ID (`TC-１５３`) was reported as `TC-`. | Every failure/error is kept (message of the first; details list each one); warnings for a suite-level `tc-id` and nested testcases; `+hhmm` and space-separated timestamps are read, unreadable ones are reported in `warnings`; the declared reference keeps any letters/digits. Decision #5 addendum. | U `TestParseKeepsEveryFailureOfATestcase`, `TestParseWarnsAboutIgnoredStructure`, `TestParseSuiteTimestampNotices`, `TestExtractRef`, `TestIngestWarnsWhenTheSuiteTimestampIsLaterThanIngestion`; probe |
| 21 | Test Result Persistence (sweep) | The history of a test case with 100k results did not answer deep pages in 120 s (per-run counts were computed for every row skipped by `OFFSET`, and each run's outcome once per result on the page); the run list had the same `OFFSET` pattern. | Pages are chosen first and each run's counts and outcome are computed once: last history page 0.02 s, first 0.27 s, run list 0.1 s with a 100k-result run. | U `TestHistory` (one summary read per run); I BE-INT-014 (250 results, deep page, counts) |
| 22 | Test Case Data Model (sweep) | The database accepted a blank title, description/expected result over 10000 characters, `deprecated` without `deprecated_at` (and the reverse) and `updated_at` before `created_at`. | Migration 00011: CHECKs mirroring the service rules. | I BE-INT-026 |
| 23 | CI/CD Result Ingestion (sweep) | The expected universe and the status of the referenced TC-IDs were read in two queries: a deprecation committed in between could put a test case in the snapshot but correlate its result as deprecated (or the reverse). | One catalog read (`ListIngestionView`) gives both from the same snapshot. | I BE-INT-027 (30 concurrent ingest + deprecate pairs), BE-INT-005 |
| 24 | CI/CD Result Ingestion API | A request missing several parameters only reported `runAttempt`; a `charset` in the Content-Type was ignored (a Latin-1 report without declaration was a 400); `Content-Encoding: gzip` gave a misleading "illegal character U+001F" 400; `+xml` media types were a 415; a report over nginx's 11 MB limit got nginx's HTML 413 page instead of problem+json. | All parameter errors come at once; the Content-Type charset overrides the XML declaration (RFC 7303; unsupported charset = 415); any `Content-Encoding` other than identity is a clear 415; `text/xml` and `*+xml` are accepted; nginx answers its 413 with the API's problem+json. | U `TestIngestHandlerReportsEveryParameterError`, `TestIngestHandlerMediaTypes`, `TestIngestHandlerRejectsCompressedBodies`, `TestParseWithCharsetOverridesTheDeclaration`; C `TestIngestionMediaTypes`; I BE-INT-012 (replay); probe (12 MB through nginx, in CI) |
| 25 | TestRun Detail UI | A result's `errorDetails` (stack trace, and since finding 20 every failure of the testcase) only existed as the `title` tooltip of a truncated message: unreachable on touch screens and by keyboard. The app had no favicon, so every page load logged a 404 (`/favicon.ico`; nginx answered it with the SPA's HTML). | Each result with details gets an accessible toggle (`aria-expanded`) that opens a full-width, scrollable, wrapped `<pre>` row; the message wraps to two lines instead of truncating. A favicon (the app's flask) is linked; the unused Vite scaffold icons were removed. | I FE-INT-011, FE-INT-021; E FE-E2E-009 (no console errors or failed requests); screenshot 35 |

## Acceptance criteria that lacked an explicit test (added)

| Card | AC | Added test |
|---|---|---|
| TestRun Data Model & Lifecycle | A replay does not recompute the snapshot (only unit-tested with a fake) | I BE-INT-008: the catalog changes before the replay and the snapshot stays at 1 |
| TestRun Data Model & Lifecycle | Supports the created/running/completed/interrupted/cancelled lifecycle | I BE-INT-011: the DB accepts all 5 statuses and rejects others (including the old `failed`) |
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
| | Lifecycle created/running/completed/interrupted/cancelled (API: completed/interrupted/cancelled) | I BE-INT-011, BE-INT-019 |
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
