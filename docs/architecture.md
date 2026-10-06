# Architecture

## Modular monolith

One Go process, eight modules with their own internal interfaces. No queues, RPC or service clients.

| Module | Owns (tables) | Public interface | Depends on |
|---|---|---|---|
| `catalog` (Test Catalog) | `projects`, `test_cases`, `test_steps`, `classification_dimensions`, `classification_values`, `test_case_classifications`, `test_case_tags`, `test_suites`, `test_suite_cases`, `requirements`, `requirement_test_cases`, `issues`, `issue_test_cases` | `catalog.Service` | `execution` only through `ResultReader` (latest results for coverage and verification) |
| `execution` (TestRun/Execution) | `test_runs`, `test_run_expected_cases`, `test_results`, `test_run_parse_errors`, `test_run_amendments`, `test_run_events` | `execution.Service` | `catalog` only through `TestCaseChecker` (404 on history) |
| `ingestion` | none | `ingestion.Service` (JUnit, manual and live runs) | `catalog` (`ExpectedUniverse`, `Statuses`), `execution` (`RecordRun`, `Diagnostics`) |
| `identity` | `users`, `invitations`, `project_members`, `api_keys`, `personal_access_tokens` | `identity.Service`, `identity.Protect` | `catalog` (project keys) |
| `insights` | none | `insights.Service` (quality indicators) | `catalog`, `execution` |
| `audit` | `audit_events` (append-only) | `audit.Service`, `audit.Wrap` (router) | `identity` (who called, admin check) |
| `integrations` | `webhooks`, `webhook_deliveries`, `github_connections` | `integrations.Service` | `catalog` (`ProjectByKey`, `ProjectByID`, `ImportIssues`), `identity` (access); hears completed runs from `ingestion` through `RunNotifier` |
| `mcp` | none | `POST /api/v1/mcp` | the REST API in-process, with the caller's credentials |

- A module never reads another module's tables and there are **no cross-module foreign keys**: execution stores
  TC-IDs by value, keeping a future extraction possible. Known exception: identity's `project_members`, `api_keys`
  and `invitations` reference `projects (id)`.
- Each module has a `Repository` port and a `postgres` adapter over its own `sqlc` package generated from its own
  migration file only (no shared global schema).
- `internal/app` composes modules; `app.Services` exposes the application layer so REST and MCP share the same use
  cases.

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
| Snapshot amendments (DEC-42) are append-only and only for reported TC-IDs | `test_run_amendments`: unique per run and TC-ID; a trigger refuses `UPDATE`/`DELETE`, TC-IDs already in the snapshot and TC-IDs without a valid result in the run (migration 00018). The summary adds them to the universe; the run carries `amendmentCount` |
| A run's identity and history are permanent | trigger forbids deleting runs and changing `external_run_id`, provider, run id, attempt, report digest (a live run sets it once, when its report completes it) or `created_at` (status and timestamps stay open for the live lifecycle); `CHECK started_at <= completed_at`; a suite timestamp later than the ingestion leaves `startedAt` unknown with a warning |
| Steps belong to one test case, ordered 1..n without gaps, at most 100 | FK to `test_cases`; `UNIQUE(test_case_id, position) DEFERRABLE` + `CHECK (position >= 1)`; the service locks the test case row to shift/renumber and to enforce the 100 limit; trigger forbids moving a step to another test case |
| Step text: action 1..2000 non-blank characters, expected result ≤ 2000 | service validation (400) backed by `CHECK`s (`test_steps_action_not_blank`, `test_steps_expected_result_length`, migration 00008) |
| Ingested results and parse errors are the source of truth | written by the transaction that creates their run, and while a manual or live run is running (recorded results, the final report completing a live run); triggers forbid inserts into finished runs, any `UPDATE` and `DELETE` (migrations 00010, 00023, 00026) |
| `untested` is never persisted | `CHECK` on `test_results.status`; derived in `execution.ComputeSummary` |
| A run's outcome is derived on read, cheaply | routes on a run authorize it by `GetRunProject` (project only); only `GET /test-runs/{id}` loads the run and computes its outcome; a live poll reads the run and its summary inputs once (BE-INT-066, card #44) |
| `testCaseId` only for valid or deprecated correlations (deprecated results stay in history) | `CHECK ((correlation IN ('valid','deprecated')) = (test_case_id IS NOT NULL))` |

## Identity and sessions (module `identity`)

- Local accounts (MVP D13): username (lower-case, immutable) + bcrypt password (cost 12, at least 10 characters counted as characters, at most 72 bytes: bcrypt's
  limit, never truncated; the forms say so).
  Users are never deleted (trigger). The first administrator is created on start when there are no users, from
  `PROVENLY_ADMIN_USERNAME` / `PROVENLY_ADMIN_PASSWORD`; prod refuses the public demo password.
- Offboarding (card #61): administrators deactivate a user (`users.deactivated_at`): every request re-reads the user,
  so their sessions are refused at once, and sign-in answers like a wrong password after the same bcrypt work.
  Nobody deactivates themselves and the last active administrator stays. Project API keys belong to their project and
  keep working. Forgotten passwords get single-use reset links (`password_resets`: SHA-256 of the token only, 24 h,
  locked on use, a newer or used link voids the others; rows are only ever marked used, by trigger). The
  `provenly reset-password <username>` command is the break-glass way back in.
- New people join through single-use invitation links (7 days; email optional). Only the SHA-256 of the token is
  stored; the link is shown once. Accepting locks the invitation row, so one link creates one account.
- Sessions are HS256 JWTs (12 h) signed with `PROVENLY_JWT_SECRET` (required in prod, ≥ 32 bytes; random per start
  elsewhere), sent as `Authorization: Bearer` or the HttpOnly, SameSite=Strict `provenly_session` cookie (Secure behind
  TLS: the web container passes the TLS proxy's `X-Forwarded-Proto` through). Every request re-reads the user; a
  password change invalidates older sessions (password-version claim). The web container adds a strict
  Content-Security-Policy, `X-Frame-Options: DENY`, `nosniff` and `Referrer-Policy: no-referrer`
  (`frontend/security-headers.conf`).
- `identity.Protect` wraps the routes of the other modules: every API route needs a session except health, readiness,
  sign-in, sign-out and accepting an invitation. Without a session the answer is `401 unauthorized`;
  administrator-only operations answer `403 forbidden`.
- CI reports with project API keys (`pvk_...`, `Authorization: Bearer` only; SHA-256 stored, shown once).
  `identity.ProtectWithKeys` wraps ingestion: a key authenticates as the key (no user) and `Require` lets it report
  into its own project only; every other route rejects keys. Maintainers create, list and revoke them.
- People's scripts and MCP clients read with personal access tokens (card #62, `pvly_pat_...`, `Authorization:
  Bearer` only; SHA-256 stored, shown once; `personal_access_tokens` + `personal_access_token_projects`, never deleted
  or edited, by trigger). A token belongs to one person and names 1 to 50 of their projects; it always expires (default
  90 days, at most 365, enforced by a CHECK). `identity.RequireUser` authenticates it as its person and puts the token
  in the context: only GET and MCP pass (anything else `403`, "read-only"); `Require` adds "the token covers the
  project" after the role check (a hidden project is still `404`, a visible one outside the token `403`); `Scope`
  narrows lists to the token's projects; `requireAdmin` refuses every token (`403`). Revoked, expired or a deactivated
  person's token: `401`. Deactivation revokes the person's tokens. Creating and revoking need a session and are
  audited; `last_used_at` is written at most once a minute.

## Roles (package `platform/authz`)

- Administrators (`users.is_admin`) can do everything. Other users hold one role per project in `project_members`:
  maintainer > member > viewer. `authz.Guard` (implemented by `identity`) answers `Scope` (visible projects and roles)
  and `Require(project, min role)`; catalog and execution depend only on the port.
- A project without a role is invisible: its resources answer the module's own `404`; a role below the minimum answers
  `403 forbidden`. Lists filter in SQL by the visible project ids (`NULL` means every project).
- Minimum roles: viewer reads; member writes test cases and steps; maintainer deprecates, reactivates, renames the
  project and manages members; administrators create projects and manage users and invitations.

## REST conventions (see `api/openapi.yaml`)

- Errors: `application/problem+json` with a stable `code` (`validation_error`, `not_found`, `invalid_junit`, …) and
  optional field `errors`. Internal errors never leak details.
- Project keys (card #45): `platform/projectkey` holds the one key pattern, the one field message for a malformed key
  (400 `validation_error`) and the one 404 for an unknown or invisible project (`project <KEY> not found`), whether the
  key comes in the path, `?project=`, a body field or the ingestion; `projectkey.Resolve` looks the key up and checks the
  caller's role, so every module resolves a project the same way.
- `GET /api/v1/test-cases?key=<PROJECT>-<number>` (card #53) finds a test case by its key: zero or one item, combinable
  with the other filters; a key of an unknown or invisible project gives no item (not a 404), anything that is not a
  key is a 400. The MCP `search_test_cases` tool takes it too.
- Pagination: `page` (1-based) and `pageSize` (1..100, default 20); responses carry `items`, `page`, `pageSize`,
  `totalItems`, `totalPages`. Every list is paged, the project's suites, requirements, issues, dimensions and webhooks
  included (DEC-78); counts a screen needs over the whole list come with the page (`coverageCounts`,
  `verificationCounts`) instead of fetching every page. Pickers search on the server: `GET /api/v1/test-cases?q=`
  matches the title (case-insensitive, `%` and `_` literal) or a key or number (`chk-12`, `12`); blank or over 200
  characters is a 400.
- Query parameters: unknown ones are ignored; every known parameter is applied and validated (an invalid value of a
  known parameter is a `400` even when unknown ones are present). A known parameter present but empty (`status=`) is
  invalid; a repeated one uses its first value. Exception (card #55): the ingestion's `pipeline`, `branch` and `commit`
  take an empty value as absent, because CI templates expand unset variables to empty (`branch=${GITHUB_HEAD_REF}`
  on a push build; prefer `${GITHUB_HEAD_REF:-$GITHUB_REF_NAME}`). A page whose offset does not fit the database is a `400`.
- Path segments: ids, `{projectKey}` and `{username}` are checked against their format before any lookup, so a
  malformed one (NUL, invalid UTF-8, wrong case or length) is a `400 validation_error` on every route, never a `500`.
- Request bodies: JSON operations require `Content-Type: application/json` (`415 unsupported_media_type` otherwise);
  duplicate keys keep the last value. Text fields (JSON or query) must be valid UTF-8 without NUL characters (`400`
  otherwise), since PostgreSQL cannot store them. Every validation error has the detail `request validation failed` and lists
  the offending fields in `errors`.
- Optimistic locking (MVP D7): test case and step responses carry `ETag: "<version>"`; their writes accept `If-Match`
  and answer `412 precondition_failed` when the version moved. Database triggers advance the version on every content
  or step change; the service checks the precondition under the row lock in the write's transaction.
- Ingestion: `POST /api/v1/ingestion/junit?provider=&runId=&runAttempt=[&pipeline=&branch=&commit=]` with the
  JUnit XML as `application/xml` body (one request per complete report; `text/xml` and `*+xml` too; a Content-Type
  `charset` overrides the XML declaration; `Content-Encoding: gzip` is accepted with the size limit on the decompressed body (a broken gzip stream is `400 invalid_junit`), other encodings are a 415; every parameter error is listed at once). `201` creates the run, `200` is an idempotent
  replay (nothing re-processed).
- Sharded runs (card #57): `?shard=i/N` (2 ≤ N ≤ 100) makes a report one of the N of a logical run (same provider,
  runId and runAttempt). The first shard creates the run (mode `sharded`, `running`, snapshot taken then); each shard is
  a row of `test_run_shards` (digest, status; append-only, accepted only while the run runs and within `shard_total`)
  and its results and parse errors carry it. Shards are serialized by the run's row lock; the one that completes the
  set finishes the run with the worst shard status (cancelled > interrupted > completed) and fires `run.completed` once.
  A shard answers for its own report (results count, diagnostics, parse errors, replay digest and status). A different
  N, a shard for an unsharded run (or the opposite) and a shard after the end are 409. `POST /api/v1/ingestion/finalize`
  ends a waiting run as interrupted with its missing shards named; `GET …/results?shard=` filters by shard.

## Taxonomy (prototype feature 9)

Each project classifies test cases along dimensions (`classification_dimensions`) with controlled values
(`classification_values`); a test case has at most one value per dimension (`test_case_classifications`, primary key
`(test_case_id, dimension_id)`) and free tags (`test_case_tags`). Composite foreign keys keep a value inside its
dimension and the dimension inside the test case's project; triggers forbid deleting dimensions or values, changing
their keys or moving a dimension. A trigger on `projects` seeds the built-in dimensions. Tag and classification
writes advance the test case version through the same trigger as steps, so they follow If-Match. The catalog
service resolves keys to ids and validates them (archived values only when already current).

## Suites and partial runs (prototype feature 10, MVP D2)

`test_suites` (static or query) and `test_suite_cases` belong to the catalog. Ingestion resolves `?suite=` through
`catalog.Service.SuiteSelection` (active automated test cases the suite selects; archived suites are a 409) and
intersects it with the expected universe read in the same snapshot as the correlation; the run stores the suite's
key and name (`test_runs.suite_key/suite_name`, immutable). Summaries need no change: results outside the suite are
outside the universe, as before.

## Manual execution (prototype feature 11, MVP D3)

A manual run is a test run with `mode = manual` and status `running` until a person finishes it. The ingestion module
orchestrates it (`ingestion.Manual`: project access, selection through `catalog.Service.Selection`, the actor from
identity) and the execution module stores it (`StartRun`, `RecordResult`, `FinishRun` under a row lock). Results stay
append-only: the `test_run_children_immutable` trigger admits inserts only in the creating transaction or while a
non-batch run is running, and `test_runs_protect_identity` freezes a finished run's status. Manual results use the
class name `provenly-manual`, so a re-test is the next attempt of the same test and is never counted as flaky.

## Requirements and traceability (prototype feature 12)

`requirements` and `requirement_test_cases` belong to the catalog. A requirement is native (provider `provenly`,
numbered `R-<n>` from `projects.next_requirement_number`, so concurrent creations never collide) or mirrored from an
external tool by provider and external id (`UpsertRequirement`: an import creates or updates by that key and stamps
`last_synced_at`; links and archiving survive re-imports). Coverage is read, never stored: the catalog asks a
`catalog.ResultReader` port (implemented by `execution.Service.LatestStatuses`, wired in `app`) for the logical status
of each covering test case in the latest run that has a valid result for it, and derives uncovered / not_run /
failing / partial / passing. The catalog stays independent of the execution module.

## Issues and verification (prototype feature 13, DEC-8)

`issues` and `issue_test_cases` belong to the catalog and mirror requirements: native (`I-<n>` from
`projects.next_issue_number`) or federated from a tracker by provider and external id, with a normalized `state`
(open / closed, `closed_at` kept consistent by a check constraint). Verification is derived on read: the
`catalog.ResultReader` port also offers `LatestConclusive` (execution query `ListLatestConclusive`: per test case, the
latest run whose logical status is passed, failed or error, computed in SQL with the same last-attempt and
failed > error > skipped > passed rules as summaries), and the base matrix of DEC-8 maps state + evidence per link;
the issue takes the worst link (reopen > known_issue > unverified > not_reproducible > validated_fixed). The run page
splits its failures into known issues (linked to an open issue) and new failures.

## Insights (prototype feature 14)

`internal/insights` is a read-only orchestrator like ingestion: it owns no tables. `GET /projects/{key}/quality`
checks project access (identity), reads the active automated and manual test cases (`catalog.Service.Selection`),
when each last executed (execution query `ListLastExecuted`) and how many of the latest runs each was flaky in
(`ListFlakyCounts`: last attempt passed after a failed or errored one, manual re-tests excluded). The dashboard page
combines it with the run list (trend), requirement coverage and issue verification, which stay in their modules.

## Live runs (prototype feature 15)

A live run is a test run with `mode = live`, started by `ingestion.Live` (same identity, project access and API keys as
reports; snapshot taken at start) and `running` until its final JUnit report arrives through the normal ingestion:
`execution.Service.RecordRun` finds the running live run by its external id and, under its row lock, appends the
report's results and parse errors and completes it (`CompleteLiveRun`: execution status, completion time and the
report digest, which the identity trigger lets a running live run set exactly once). Events live in
`test_run_events` (append-only, unique per run and event id, accepted only while the live run runs, at most 10,000
per run); `execution.Service.Live` derives the provisional state of each expected test case and, once the run is
finished, reconciles the events with the run summary (status mismatch, live-only, final-only,
started-without-finished, duplicate, invalid correlation). The UI polls every two seconds while the run is running.

## Playwright reporter (prototype feature 16)

`reporters/playwright` is a client of the public API only (live runs, events and JUnit ingestion with a project API
key): it adds no server code. Its TC-ID conventions mirror the JUnit ones (`tc-id` property, KEY-n in the name).

## Observability (prototype feature 17)

`internal/platform/telemetry` installs the OpenTelemetry SDK at startup (always, so spans and log correlation exist;
exported over OTLP/HTTP only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set). `telemetry.Middleware` wraps the API
(otelhttp: continues `traceparent`, names spans after the mux route, sets `X-Trace-Id`); `postgres.Open` adds the
otelpgx tracer so queries are child spans; `ingestion.Service.IngestJUnit` opens the ingestion span; the slog handler
adds `trace_id`/`span_id` to every record written with a request context.

## Integrations (prototype feature 18, MVP D4)

`internal/integrations` owns webhooks and the GitHub Issues connector. Secrets Provenly must read back (webhook
signing secrets, connector tokens) are sealed with AES-256-GCM by `platform/secrets` under `PROVENLY_SECRETS_KEY`
(32 bytes, base64; required in prod, random with a warning elsewhere) and stored as `v1:` + base64; the database
refuses anything else. Secrets Provenly only verifies (passwords, API keys) stay hashed.

- **Outbox**: ingestion (a created batch run, a live run completed by its report) and `Manual.Finish` call
  `RunNotifier.RunCompleted` after the run is committed, which inserts one `webhook_deliveries` row per active
  subscribed webhook; it never fails the recording (enqueueing is at most once: a crash between the two loses that
  run's event). A worker started by `serve` (`Integrations.Run`, every 2 s) claims due rows with
  `FOR UPDATE SKIP LOCKED` and a five-minute lease (longer than a pass of 20 deliveries at 10 s each), POSTs them
  signed (`X-Provenly-Signature: sha256=HMAC(ts.body)`) and records the attempt only if the row is still the one it
  claimed (a late worker whose lease expired changes nothing): succeeded on 2xx, else retried after 10 s, 1 min,
  5 min, 30 min, then failed.
- **Retention** (card #51): the same worker purges, at start and hourly, finished deliveries completed more than
  `PROVENLY_WEBHOOK_DELIVERY_RETENTION_DAYS` ago (default 90; `0` keeps them) in batches of 1,000. Each batch is one
  statement guarded by `pg_try_advisory_xact_lock`, so a second server purging at the same time deletes nothing.
  Pending deliveries, audit events and run results are never purged.
- **SSRF**: outbound requests (webhooks and GitHub) use a client that refuses private, loopback and link-local
  addresses after DNS resolution (dialer `Control`), follows no redirects and ignores proxies; `PROVENLY_WEBHOOKS_ALLOW_PRIVATE`
  (default: everywhere but prod) lifts it for local endpoints.
- **GitHub**: `POST /projects/{key}/github/sync` reads up to 5 pages of 100 issues (`PROVENLY_GITHUB_API_URL`,
  default the public API) and mirrors them through `catalog.ImportIssues` (provider `github`, external id = number);
  failures are 502 `upstream_error` and recorded on the connection; a token another key sealed is 409.

## Agents: MCP (prototype feature 19, DEC-10)

`internal/mcp` serves `POST /api/v1/mcp` (Streamable HTTP, JSON-RPC, JSON responses) on the session routes. It owns
no use cases: each tool is a GET on the application's own router (`app.NewHandler` binds it after registering the
routes) with the caller's `Authorization`/`Cookie`, so authorization, validation and JSON shapes are the REST ones
(a personal access token reads through MCP exactly as through REST: its projects only).
Path arguments must be key-shaped (no `/` or dot segments) so a tool cannot address another route.

## Audit log (prototype feature 20)

`internal/app` registers the session and API key routes through `audit.Wrap`, which wraps each non-GET handler
*inside* authentication (identity's router wraps it in turn), so the caller is in the context when the handler
returns. A 2xx answer writes one `audit_events` row (actor, method + route pattern, path, project key, status) after
the response; the write never fails the request. MCP and live events are skipped. A trigger makes the table
append-only.

Card #48 makes entries readable. The router opens an `auditnote` in the request context; `identity.Service.Require`
writes the project it allowed (the first one), so a change whose path names no project (a test case, a step, a run,
a body's `project`) is filed under it. Before the handler runs the router resolves, through `audit.Resolver`
(catalog lookups that never check access, used only for changes already allowed), the test case key and step position
the route names, then stores a `summary` ("deleted CHK-4 step 3", from the `summaries` table of route patterns) and the
`test_case_key` (`GET /audit?testCase=CHK-4`). Events recorded before have neither and are not backfilled: the table is
append-only.

Card #49 records sign-in events, which have no signed-in caller: `identity.Service` reports them through its
`AuthLog` port (implemented by `audit.Service`) — a sign-in that succeeds, fails (401) or is locked out (429), a
sign-out with a valid session, an accepted invitation, a password reset — with the account's username, or `unknown`
when it matches none (a typed username or password is never stored). Every event, changes included, keeps the client
IP and user agent from `platform/clientinfo`: the peer address, or the nearest `X-Forwarded-For` hop added by a proxy
in `PROVENLY_TRUSTED_PROXIES` (none by default). Each sign-in event is also a structured log line
(`event=auth.login_failed actor=… ip=…`).

## Releases and sign-in throttle (prototype feature 21)

`identity.Service.Login` consults an in-memory throttle keyed by the normalized username before checking the
password (so a locked account costs no bcrypt), counts `invalid username or password` answers and clears on success;
`apperr.KindTooManyRequests` maps to 429 `too_many_requests`. Release images are built by `release.yml` with
`VERSION` stamped into `app.Version`.

## Retries (MVP D1)

Each result stores its `attempt` (from Surefire flaky/rerun elements or an `attempt`/`retry` property). A test is its
suite + class + name in the run; its highest attempt is its logical result (flaky when every result of it passed after a
failed or errored attempt). Repeated names without an attempt signal share their attempt and stay variants (the
ingestion warns how many). The summary aggregates logical results per TC-ID with failed > error > skipped > passed, and
a TC-ID whose aggregate is failed or error is never flaky (a variant failed for good); `ListFlakyCounts` and
`ListLatestConclusive` apply the same rules in SQL.

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
