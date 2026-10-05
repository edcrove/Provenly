# Provenly — full-product exploration prototype

Branch `prototype/full-product` (from `main` at 5245a4c, 2026-10-05). Ed asked for the **whole product** built from
everything written so far (Trello backlog, Notion Incubator, Planning Backlog, Decision Register, product pages),
including ideas that were never refined, **without his intervention**: every open question is decided here, with the
reasoning, so the prototype can be reviewed afterwards and the real backlog refined from it.

Rules kept from `main` (CLAUDE.md): contract first, a goose migration per schema change, tests in every layer touched on
both sides, the 8 + 2 coverage gates at 100%, `make lint` clean. One PR per feature into this branch, self-reviewed.
Nothing here is merged into `main`.

Status legend: ✅ merged into the prototype branch · 🚧 in progress · ⏳ planned.

## Feature map

| # | Feature | Source | Status | PR |
|---|---|---|---|---|
| 1 | Projects and per-project TC keys (`CHK-12`) | MVP D9, D10 | ✅ | proto/01-projects |
| 2 | Users, login (JWT) and invitations | MVP D13, DEC-30 | ✅ | proto/02-auth |
| 3 | Roles and project membership (Admin, Maintainer, Member, Viewer) | MVP D12 | ✅ | proto/03-roles |
| 4 | API keys for CI, `?project=` ingestion (secrets at rest moved to 18, see P4-6) | MVP D4, D11 | ✅ | proto/04-api-keys |
| 5 | Optimistic locking (ETag / If-Match) | MVP D7 | ✅ | proto/05-optimistic-locking |
| 6 | Snapshot amendment per run | DEC-42 | ✅ | proto/06-snapshot-amendment |
| 7 | Retries, logical result and flaky | MVP D1 | ✅ | proto/07-retries-flaky |
| 8 | Compressed (gzip) report ingestion | MVP D6 | ✅ | proto/08-gzip-ingestion |
| 9 | Taxonomy: tags and custom dimensions | Planning #26 | ✅ | proto/09-taxonomy |
| 10 | Test suites (static and query) and partial-run scope | Planning #27, MVP D2, Incubator | ✅ | proto/10-suites |
| 11 | Manual execution (manual runs, step results) | MVP D3, Planning #4, Incubator | ✅ | proto/11-manual-execution |
| 12 | Requirements and requirement ↔ test traceability | Incubator (Requirements Federation) | ✅ | proto/12-requirements |
| 13 | Issues, known issues and issue verification | Incubator, Planning #8 | ⏳ | |
| 14 | Quality dashboard (trends, flaky, coverage) | Incubator (Quality Intelligence) | ⏳ | |
| 15 | Live runs: execution sessions, live events, reconciliation | Trello Live Streaming, Planning #9 | ⏳ | |
| 16 | Playwright reporter (`@provenly/playwright-reporter`) | Trello, DEC-15 | ⏳ | |
| 17 | OpenTelemetry basic instrumentation | Trello, DEC-11 | ⏳ | |
| 18 | Export sink (webhooks) and GitHub connector, secrets at rest | Planning #3, #21, Incubator, MVP D4 | ⏳ | |
| 19 | MCP server (agent interface) | Incubator, DEC-10 | ⏳ | |
| 20 | Audit log | Incubator (Project & Authorization) | ⏳ | |
| 21 | Release pipeline, self-hosting guide, dogfooding, public readiness | Trello phase 4 | ⏳ | |

## Decision log

Decisions taken in the prototype without Ed (to review). `MVP Dn` and `DEC-n` are Ed's decisions, applied as written.

| Id | Topic | Decision | Why |
|---|---|---|---|
| P1-1 | Project key format | 2–10 upper-case letters/digits starting with a letter; trimmed and upper-cased on create; never changes; projects are never deleted | Keys are printed in test names and reports, so they must be stable and readable like Jira keys |
| P1-2 | Existing data | Default project `TC` (id 1) holds every test case created before projects; existing ids become numbers (`TC-153` stays `TC-153`); its counter continues after the highest id | No reference already written in a test or report breaks |
| P1-3 | URLs and internal ids | REST paths keep the numeric internal id (`/test-cases/{id}`); `key`, `projectKey` and `number` are added to the payload | Smallest change to the contract; the key is for humans and reports, the id for links |
| P1-4 | Numbering | Per-project counter incremented in the same statement as the insert (row lock); contiguous under concurrency, never reused | Same guarantees as the old identity column, now per project |
| P1-5 | Ingestion | `?project=<KEY>` (default `TC`) chooses the run's project and its expected universe; unknown key 404 | MVP D11 (ingestion project as a parameter) |
| P1-6 | References | A `tc-id` property may carry any key; another project's key is a new `wrong_project` diagnostic, never correlated. The name fallback only reads the run's own key; a bare number belongs to the run's project | Avoids silent cross-project links and false positives in test names (`HTTP-200`) |
| P1-7 | Run identity | `externalRunId` unique per project | One CI pipeline may report several projects |
| P1-8 | Keys in run responses | Results, summaries and history carry `testCaseKey` (resolved by the catalog through its public interface) | Runs reference internal ids; showing `TC-<id>` would be wrong for other projects |
| P2-1 | Bootstrap | The first administrator comes from `PROVENLY_ADMIN_USERNAME` / `PROVENLY_ADMIN_PASSWORD` when there are no users; demo and qa ship a published demo password, prod refuses it | Self-hosting needs a first account without a setup wizard race; config is how the other settings already arrive |
| P2-2 | Session transport | HS256 JWT (12 h) returned by sign-in and also set as an HttpOnly, SameSite=Strict cookie; the API accepts `Authorization: Bearer` or the cookie | The browser never sees the token (no XSS theft), scripts and CI tools use the bearer form; SameSite=Strict plus JSON-only bodies covers CSRF |
| P2-3 | Revocation | Stateless sessions, but each request re-reads the user and a password-version claim signs every other session out on a password change | Real revocation without a session table; per-request user read is needed for roles anyway (feature 3) |
| P2-4 | Passwords | bcrypt cost 12, 10 characters to 72 bytes (bcrypt's limit, never truncated silently); one answer for unknown user and wrong password, with equal timing | NIST-style minimum length, no composition rules; no account enumeration |
| P2-5 | Invitations | Single-use link, 7 days, optional email (prefills the account's email), revocable while pending; only the SHA-256 of the token is stored; shown once | MVP D13 (invitation link, email optional); a leaked database does not leak usable links |
| P2-6 | Who may invite | Only administrators manage users and invitations in this feature; feature 3 replaces this with roles | Smallest rule until roles exist |
| P2-7 | Ingestion | Stays public until CI API keys exist (feature 4) | CI cannot sign in with a person's account; keys are the MVP answer (D4, D11) |
| P2-8 | Usernames | 3–32 lower-case letters, digits, `.`, `-`, `_`; immutable; users are never deleted | Usernames appear in audit and history; a rename would rewrite who did what |
| P2-9 | Known limits | No rate limiting or lockout on sign-in, no password reset by email, no SSO | Recorded for the readiness feature (21); reset is an admin action today (invite again) |
| P3-1 | Role model | Global administrator flag plus one role per project: maintainer > member > viewer | MVP D12; administrators stay global so a fresh install has someone who can do everything |
| P3-2 | What each role may do | viewer reads; member creates and edits test cases and steps (and marks them automated); maintainer also deprecates, reactivates, renames the project and manages its members; creating projects and inviting people is admin-only | Deprecation and membership change what others see, so they sit one level up; project creation is an installation concern |
| P3-3 | Invisible vs forbidden | A project without a role is invisible: its test cases, runs, members and history answer 404 and lists skip it; a role that is too low answers 403 | Never confirms that another project's resources exist; 403 only where the caller can already see the resource |
| P3-4 | Lists | Test case, run and project lists are filtered in SQL by the visible project ids (`NULL` = every project for administrators) | Correct pagination and counts; no post-filtering |
| P3-5 | Joining a project | An invitation may carry a project and role, applied in the same transaction that creates the account; afterwards administrators or the project's maintainers add members by username | One link onboards someone into the right project |
| P3-6 | Ingestion | Still public; runs are attributed by `?project=` (feature 4 binds CI keys to a project) | Same reason as P2-7 |
| P3-7 | UI | Each project carries `myRole`; the UI hides actions the role cannot do (Edit, Deprecate, New test case, Rename, member controls) and the API still enforces them | Hidden-but-enforced: the UI is a convenience, the API is the rule |
| P4-1 | CI authentication | Project API keys `pvk_<8 hex>_<43 base64url>` sent as `Authorization: Bearer`; only the SHA-256 is stored, the key is shown once | High-entropy random keys need no slow hash; the public prefix tells keys apart in lists and logs |
| P4-2 | Key scope | A key belongs to one project and only opens ingestion; it reports into its project only (another project is 404), and its project is the default `project` | Least privilege: a leaked CI secret cannot read data or touch other projects; no extra parameter in CI |
| P4-3 | Who manages keys | Maintainers and administrators of the project create, list and revoke keys; revocation is immediate; keys are never deleted | Keys are project configuration; history of who created which key is kept |
| P4-4 | People reporting | Ingestion also accepts a session with the `member` role (viewers 403) | Manual uploads, scripts and the demo seed keep working; same rule as creating test cases |
| P4-5 | Last use | `lastUsedAt` written at most once a minute | Lets maintainers spot unused keys without a write per report |
| P4-6 | Secrets at rest (D4) | Moved to feature 18 (webhooks / GitHub), the first feature that stores a secret Provenly must read back | API keys and invitations are hashed, never encrypted; envelope encryption without a consumer would be untested code |
| P4-7 | Key transport | Keys are read only from the `Authorization` header, never from the session cookie | Found by the probe: a key placed in the cookie was accepted; cookies are browser sessions |
| P5-1 | Unit of locking | One version per test case covering its content and its steps (`test_cases.version`); step reads and writes use the test case's ETag | People edit "the test case"; a step reorder racing a title edit is the same conflict. One counter, one ETag |
| P5-2 | Who advances it | Database triggers: any content change of the test case and any step insert, update or delete advance it; a no-op save does not; it never goes back | No write path (today's or a future one) can forget to; checked in the same transaction that locks the row |
| P5-3 | Contract | `ETag: "<version>"` on test case and step responses, `version` in the test case body; writes accept `If-Match` (`*`, a list of tags; weak tags never match) and answer `412 precondition_failed`; malformed is 400; without `If-Match` last write wins (D7) | RFC 9110 semantics; CI tools and scripts keep working without it |
| P5-4 | UI | Every test case and step write sends `If-Match` with the version on screen; step writes carry the new ETag forward; a 412 shows "Someone else saved this test case" with Reload | D7: the UI always sends it; successive own edits never conflict with themselves |
| P5-5 | Partial saves | The edit form sends only the fields the user changed since opening it | After a conflict and Reload, saving does not overwrite the other person's change in other fields; found while writing the E2E |
| P5-6 | Projects | Not versioned in this feature | D7 covers test cases and steps; project renames are rare and maintainer-only |
| P6-1 | Shape | Amendments are a separate append-only list per run (who, when, why); the snapshot table never changes; the summary computes over snapshot + amendments | DEC-42: "an amended summary is never mistaken for the original snapshot"; the original stays queryable (`snapshotTotal`) |
| P6-2 | Eligibility | Only TC-IDs with a valid result in the run that were outside its snapshot (the summary's `outsideUniverseTestCaseIds`); enforced by the service and a database trigger | Includes what CI actually reported; never invents untested entries |
| P6-3 | Who | Maintainers and administrators of the run's project; a reason (1–500 characters) is required | It changes the official numbers of a run |
| P6-4 | Visibility | `amendmentCount` on every run (list, detail, history), an "edited" badge, an "Edited after creation" card with the history, and the summary splits "N in the snapshot + M included later" | DEC-42 asks for a visible mark in list, detail and API |
| P6-5 | No undo | An amendment cannot be removed | Audit trail; a mistaken inclusion is visible with its reason. Revisit with the audit log (feature 20) if needed |
| P6-6 | Actor | `authz.Guard.Actor` gives the signed-in user; API keys cannot amend | Amendments are human decisions |
| P7-1 | Detecting retries | Only when the report says so: Surefire `<flakyFailure>`/`<flakyError>` (failed attempts before a pass) and `<rerunFailure>`/`<rerunError>` (attempts after a failure), or an `attempt` (1-based) / `retry` (0-based, Playwright) testcase property. Repeated names without a signal stay variants | Treating every repeated name as a retry would silently turn failed variants into flaky passes in existing reports |
| P7-2 | Test identity | A test is its suite + class + name within the run; its attempts are numbered 1..100 | Variants (e.g. per browser) have different names and keep aggregating failed > error > skipped > passed (D1) |
| P7-3 | Logical result | The highest attempt of each test (the later one on ties); a pass after a failed or errored attempt is `passed` and **flaky** | MVP D1, whatever the cause |
| P7-4 | Storage | Every attempt is stored as a result with `attempt`; `retried` (a later attempt exists) is derived in queries; results stay immutable | Nothing reported is lost; the history shows every attempt |
| P7-5 | Exposure | `flaky` in run outcome and summary (TC-IDs), `flaky` per summary test case, `attempt` and `retried` per result; UI: flaky badge (list and detail), flaky test cases, attempt markers in results and history | Flaky passes count as passed but stay visible |
| P7-6 | Limits | Attempts beyond 100 keep the last 100 with a warning; invalid attempt/retry values are first attempts with a warning; Surefire attempt details come from `<stackTrace>`, their duration is unknown | Broken reporters never fail ingestion |
| P8-1 | Encodings | `Content-Encoding: gzip` (and `x-gzip`); no encoding or `identity` as before; anything else (br, deflate, lists) stays a 415 | D6; gzip is what CI tools produce with one command |
| P8-2 | Size limit | `PROVENLY_MAX_INGEST_BYTES` applies to the decompressed report (413 problem past it) and the compressed body is read through the same limit | D6; a gzip bomb never expands past the limit in memory |
| P8-3 | Broken streams | Not gzip, truncated or corrupt: 400 `validation_error` on `body` | Client error with the reason, never a 500 |
| P8-4 | CI step | The API key page's ready-to-paste step now gzips the report (`gzip -c junit.xml \| curl ... --data-binary @-`) | Smaller uploads by default |
| P9-1 | Model | Per-project **dimensions** with controlled **values** (key + display name) and free **tags** per test case; one value per dimension per test case | Planning #26: orthogonal dimensions, structured data for reporting, tags complement but do not replace them; multi-valued needs (several platforms) use tags |
| P9-2 | Built-ins | Every project (existing ones by migration, new ones by a database trigger) gets feature, component, level, depth, type, risk and platform; level, depth, type and risk come with standard values, feature/component/platform start empty | Planning #26 list; **execution mode is not a dimension**: it is the existing `automated` flag (one source of truth) |
| P9-3 | Stability | Dimensions and values are archived, never deleted, and their keys never change (database triggers); archived ones stay on test cases but cannot be newly assigned (re-sending the current value is accepted) | "Values used for reporting must be stable and auditable" (Planning #26) |
| P9-4 | Content | Tags and classification are test case content: part of the test case body, written with PATCH (tags replace, classification merges per dimension, `null` clears), advance the version and honour If-Match | Same optimistic locking as every other edit (P5); a dimension-level merge lets two people classify different dimensions without conflict in the UI |
| P9-5 | Tags | Trimmed, lower-cased, deduplicated, sorted; `^[a-z0-9][a-z0-9._-]{0,39}$`, at most 20 per test case | Predictable filtering; bounded input |
| P9-6 | Filters | `GET /test-cases?tag=` and `?classification=dim:value,...` (up to 10 pairs, all must hold); the UI offers the classification filter only within a project | AND is what Smart Suites (feature 10) need; dimensions are per project |
| P9-7 | Permissions | Viewers read dimensions; maintainers add, rename and archive dimensions and values; members classify and tag the test cases they can edit | Taxonomy shapes reporting for the whole project (maintainer), classifying is everyday editing (member) |
| P9-8 | Deferred | Reporter metadata suggesting a classification goes to feature 16 (Playwright reporter); reporting by dimension to feature 14; dynamic suites to feature 10 | Never overwrite human classification without a policy (Planning #26) |
| P10-1 | Model | A suite belongs to a project and is **static** (an explicit list of test cases, at most 1000) or **query** (a tag and dimension:value pairs that must all hold, at most 10); kind and key never change | Planning #26/#27 Static vs Smart suites; a query suite follows the catalog as it is classified |
| P10-2 | Partial runs (D2) | CI names the suite with `?suite=<key>` on ingestion: the expected universe is the suite's **active automated** test cases at that moment (snapshotted like before); results outside it are valid but outside the universe | Answers D2 ("a smoke run of 10 of 100 TCs must not show 90 untested") with the existing snapshot model |
| P10-3 | Run identity | The run keeps the suite's key and name of that moment (immutable like the rest of its identity); renaming the suite later does not rewrite history; runs filter by `?suite=` | Runs historically conserve their selection (Planning #26 invariant) |
| P10-4 | Lifecycle | Suites are archived, never deleted; an archived suite takes no new runs (409) and an unknown one is 404, so a CI typo never silently becomes a full run | Fail loudly instead of reporting against the wrong universe |
| P10-5 | Membership | Static members may be deprecated or manual (they simply drop out of the expected universe); members must be of the suite's project; the list `?project=&suite=` shows members or matches | Keeps the suite definition stable while the catalog changes |
| P10-6 | Permissions | Viewers read suites; maintainers create, edit, archive and change members | Suites define what CI runs count; same level as taxonomy (P9-7) |
| P10-7 | Not now | Nested suites, suite templates and per-suite reporting (feature 14 reports by suite through `?suite=`); a run for several suites at once is not supported (one key per run) | Keep the model minimal for the prototype |
| P11-1 | Model | A manual run is a test run with `mode=manual` and the new execution status `running`: a member starts it (project, optional suite, scope), records results one by one and finishes it as `completed` or `cancelled`; it reuses the snapshot, summary, verdict and history of CI runs | One run model for CI and people (Planning #4); every report, dashboard and history works unchanged |
| P11-2 | Scope | Default scope **manual**: the active test cases that are not automated (CI covers the others); `all` expects every active test case (a release sign-off); a suite narrows either | Avoids expecting automated cases a person will not execute |
| P11-3 | Results | Append-only, like CI results: recording a test case again is a re-test (its next attempt, gapless even under concurrency, at most 100); the last attempt counts and a pass after a failure is **not** flaky | Keeps the audit trail; a manual re-test after a fix is not flakiness |
| P11-4 | Details | A result carries a note (shown as the error message), the failed step (only for failed/blocked) and who recorded it; "Blocked" in the UI is the `error` status | No per-step result table in the prototype: the failed step and the note cover the need; per-step results can come later |
| P11-5 | Integrity | The database lets results into a run only in its creating transaction or while a manual (or live) run is running; a finished run keeps its status; mode and starter are part of the run's immutable identity | The same "results are the source of truth" rule as CI (findings 10) |
| P11-6 | Permissions | Members start, record and finish; viewers read; manual runs need a session (API keys only report CI runs) | Manual execution is a person's work |
| P12-1 | Model | Requirements are **federated**, not migrated: native ones (written in Provenly, numbered `R-<n>` per project) or mirrors of Jira / GitHub / Azure DevOps items keyed by provider + external id, with title, description, link and the status in the source | Incubator "Requirements Federation": teams keep their tool of record; Provenly only adds the test side |
| P12-2 | Sync | Read-only, push-based import (`POST .../requirements/import`, up to 500 items, maintainers): creates or updates by external id and stamps `lastSyncedAt`; Provenly never writes back and does not poll the tools (no credentials stored) | A connector or CI job can push from any tool without Provenly holding third-party secrets; pull connectors can come with feature 18 |
| P12-3 | Traceability | Many-to-many links between requirements and test cases of the same project, replaced as a set (`PUT .../test-cases`), editable by members | Simple to reason about and idempotent; the UI links and unlinks one at a time on top of it |
| P12-4 | Coverage | Computed on read from the **latest result** of each covering test case (its logical status in the latest run with a valid result): failing if any failed/errored, passing if all passed, not run if none has results, partial otherwise, uncovered without links | Always current, no stored aggregate to keep in sync; the latest result is what a release decision looks at |
| P12-5 | Lifecycle | Requirements are archived, never deleted; provider and external id never change (database trigger); native numbers come from a per-project counter and are never reused | Same identity guarantees as test cases |
| P12-6 | UI | A Requirements page (list with coverage, native creation and external registration), a requirement page (covering test cases, latest results, link/unlink, archive) and "Requirements" on the test case page | Traceability is visible from both sides |
| P1-9 | UI | Header "current project" selector (remembered per browser) narrows test case and run lists; Projects page creates and renames projects; new test cases pick a project | Single place to switch context; no URL change needed for the prototype |

## What each feature does

Filled in as each feature is merged: behavior, API, UI, tests, known limits.

### 1. Projects and per-project TC keys

- **Behavior**: projects own test cases and runs; each numbers its test cases with its key (`CHK-1`, `CHK-2`…).
  Runs ingested with `?project=CHK` snapshot CHK's universe and only link CHK test cases; a `tc-id` with another
  project's key is reported as `wrong_project`.
- **API**: `GET/POST /api/v1/projects`, `GET/PATCH /api/v1/projects/{projectKey}` (409 on a key in use);
  `?project=` on `GET /test-cases`, `GET /test-runs` and the ingestion; `project` in `POST /test-cases`;
  `projectId`/`projectKey`/`number` on test cases, `projectId` on runs, `testCaseKey` on results and summaries,
  `wrongProject` in diagnostic counts.
- **Schema**: migrations 00012 (projects, numbering, protection triggers, backfill) and 00013 (run project,
  per-project run ids, `wrong_project`).
- **UI**: Projects page, current-project selector in the header, project picker on new test case, project column in
  the run list, keys from the API everywhere (screenshots 36–38).
- **Tests**: BE-INT-031..035, contract scenarios for every new operation × status (backend and frontend), FE-INT-022..025,
  BE-E2E-007, FE-E2E-010, unit tests (catalog, execution, ingestion, junit, httpx, apperr, frontend storage), probe
  sweep for projects.
- **Known limits**: no project-level permissions yet (feature 3); a test case cannot move between projects; the
  project list in the header loads up to 100 projects.

### 2. Users, sign-in and invitations

- **Behavior**: every page and API route needs a session (except health, readiness, sign-in, sign-out, accepting an
  invitation and ingestion). Visiting any page signed out goes to sign-in and back. Administrators invite people with a
  single-use link; the invited person picks a username and password and is signed in. A password change signs every
  other session out.
- **API**: `POST /auth/login`, `POST /auth/logout`, `GET /auth/me`, `POST /auth/password`, `GET /users`,
  `GET/POST /invitations`, `POST /invitations/{id}/revoke`, `POST /invitations/accept`; 401 declared on every
  protected operation, 403 on administrator-only ones; `bearerAuth` and `cookieAuth` security schemes.
- **Schema**: migration 00014 (users, invitations, protection triggers: no deletes, immutable usernames, settled
  invitations never change).
- **Config**: `PROVENLY_JWT_SECRET` (required in prod), `PROVENLY_ADMIN_USERNAME` / `PROVENLY_ADMIN_PASSWORD`; demo and
  qa ship `admin` / `provenly-demo`.
- **UI**: sign-in, join-by-link, account (password change) and Users pages; header shows the user and Sign out; Users is
  visible to administrators (screenshots 39–42).
- **Tests**: BE-INT-036..038 (including concurrent acceptances), a contract sweep that every protected operation answers
  401, every new operation × status on both sides, FE-INT-026..029, BE-E2E-008, FE-E2E-011, unit tests (identity,
  config, CLI, query client, links), probe sweep with auth cases. The probe found a 500 on sign-in with a NUL in the
  username; fixed (impossible usernames are never looked up) with unit and contract regressions.
- **Known limits**: see P2-9. All scripts (README flow, smoke, probe, env checks, demo seed) sign in first.

### 3. Roles and project membership

- **Behavior**: administrators see and do everything. Everyone else sees only the projects they belong to, with the
  role they hold there (P3-2). Other projects are invisible (404); a role too low for an action answers 403.
- **API**: `GET /projects/{key}/members`, `PUT/DELETE /projects/{key}/members/{username}`; `Project.myRole`;
  invitations accept `project` + `role` and report `projectId` / `projectRole`; 403 declared on every role-gated
  operation.
- **Schema**: migration 00015 (`project_members`, invitation project and role with a both-or-neither check).
- **Code**: `internal/platform/authz` (roles, scope, `Guard` port) implemented by `identity`; catalog and execution
  handlers ask the guard before acting and pass the visible project ids to their list queries.
- **UI**: members page per project (screenshot 43), role column on Projects, role-gated actions on test cases, steps,
  runs and project pages (screenshot 44 shows a viewer), invitations with an optional project and role.
- **Tests**: authz and identity unit tests, handler authorization tests in catalog and execution, BE-INT-039..040
  (membership invariants and filtered lists), contract role scenarios for every role-gated operation on both sides,
  FE-INT-030..032, BE-E2E-009, FE-E2E-012.

### 4. CI API keys

- **Behavior**: ingestion needs a project API key (CI) or a session with the `member` role. A key reports into its own
  project (the default `project`), opens no other route and stops working the moment it is revoked.
- **API**: `GET/POST /projects/{key}/api-keys`, `POST /projects/{key}/api-keys/{id}/revoke`; `ingestJUnitReport` declares
  `apiKeyAuth`, 401 and 403.
- **Schema**: migration 00016 (`api_keys`: digest only, unique prefix, protection trigger: no deletes, only last use and
  one revocation change).
- **UI**: an API keys section on the project page for maintainers: create (key shown once with a ready-to-paste CI step),
  list with last use, revoke (screenshot 44).
- **Scripts**: the README walkthrough and the smoke test report with an API key (through nginx).
- **Tests**: identity and ingestion unit tests (key format, revocation, scope, cookie regression), BE-INT-041,
  contract scenarios on both sides, FE-INT-033, BE-E2E-010, FE-E2E-013, probe API key sweep. The probe found keys
  accepted from the session cookie (P4-7); fixed with unit and contract regressions.

### 5. Optimistic locking

- **Behavior**: two people editing the same test case cannot silently overwrite each other. The second save is refused
  (412) with a notice; Reload shows the other change and the form keeps what was typed, sending only the changed fields.
- **API**: `version` in `TestCase`, `ETag` on the test case and step operations, `If-Match` on the 7 writes, 412
  `precondition_failed`.
- **Schema**: migration 00017 (`version` column, triggers on `test_cases` and `test_steps`).
- **Code**: `internal/platform/etag` (parse and match); `catalog.Service.guarded` locks the row, checks the
  precondition, writes and re-reads the version in one transaction.
- **UI**: conflict notice with Reload (screenshot 45); copy on the definition card no longer says "never creates a version".
- **Tests**: etag, apperr, httpx and catalog unit tests; BE-INT-042 (triggers, 20 concurrent writes: one wins);
  contract 412 scenarios for every write on both sides; FE-INT-034; BE-E2E-011; FE-E2E-014; probe If-Match sweep.

### 6. Snapshot amendment per run (DEC-42)

- **Behavior**: on a run, a test case reported by CI but outside the snapshot (it was manual when the run was created)
  can be included by a maintainer with a reason. The run is marked "edited", the summary and verdict count it, and the
  history shows who included what, when and why.
- **API**: `GET/POST /test-runs/{id}/amendments`; `TestRun.amendmentCount`; summary `snapshotTotal` and
  `amendedTestCaseIds`.
- **Schema**: migration 00018 (`test_run_amendments`, protection trigger).
- **UI**: "Include in this run" next to each outside-universe test case (maintainers), edited badge in run list and
  detail, amendment history card, "N in the snapshot + M included later" (screenshot 46).
- **Tests**: execution unit tests (service, summary, handlers), identity `Actor`; BE-INT-043 (trigger, 20 concurrent
  amendments: one recorded); contract scenarios both sides; FE-INT-035; BE-E2E-012; FE-E2E-015; probe amendment sweep.
  Also fixed a flaky API key test (feature 4): tampering a key could leave it unchanged when its last character was
  already the replacement.

### 7. Retries, logical result and flaky (MVP D1)

- **Behavior**: retried tests keep every attempt; the last attempt is the test's result; a test that passed only on a
  retry counts as passed and is marked flaky (run list, run detail, summary, results, history).
- **API**: `RunOutcome.flaky`, `TestRunSummary.flaky`, `TestCaseOutcome.flaky`, `TestResult.attempt` and `retried`.
- **Schema**: migration 00019 (`test_results.attempt`, index per test of a run).
- **Ingestion**: Surefire flaky/rerun elements and `attempt`/`retry` properties (P7-1); the response's `received` counts
  attempts.
- **UI**: flaky badge, "Flaky (passed on a retry)" list, attempt badges ("attempt 1 · retried") (screenshot 47).
- **Tests**: JUnit parser (attempts, properties, limits), summary (`logical`), handler and DTO tests; BE-INT-044;
  FE-INT-036; BE-E2E-013; FE-E2E-016; probe retry sweep.

### 8. Compressed (gzip) report ingestion (MVP D6)

- **Behavior**: CI can send `Content-Encoding: gzip`; the report is ingested exactly like a plain one. The size limit
  applies to the decompressed body (gzip bombs are 413s); broken streams are 400s; other encodings stay 415.
- **API**: `ingestJUnitReport` request body description; no new operation.
- **UI**: the CI step on the API keys section uses gzip (screenshot 44).
- **Tests**: handler unit tests (encodings, exact limit, bomb, incompressible body over the limit, broken streams),
  contract (201, 400, 413, 415), BE-E2E-014, probe gzip sweep through nginx.

### 9. Taxonomy: tags and classification dimensions (Planning #26)

- **Behavior**: each project classifies its test cases along dimensions with controlled values (seven built-ins
  seeded per project, plus its own); test cases also carry free tags. Archiving keeps history meaningful; nothing is
  deleted. The list filters by tag and by `dimension:value` pairs.
- **API**: `GET/POST /projects/{key}/dimensions`, `PATCH /projects/{key}/dimensions/{dimensionKey}`,
  `POST .../values`, `PATCH .../values/{valueKey}`; `tags` and `classification` on the test case (create, read,
  PATCH); `tag` and `classification` query parameters on the test case list.
- **Data**: migration 00020 (`classification_dimensions`, `classification_values`, `test_case_classifications` with
  composite foreign keys so a value always belongs to its dimension and the test case's project, `test_case_tags`),
  version triggers reused from P5.
- **UI**: Classification section on the project page; tags and one select per dimension on the test case form; badges
  on the test case; tags column and tag/classification filters on the list (screenshots 49–52).
- **Tests**: unit (service, handlers, helpers), BE-INT-045, backend and frontend contract, FE-INT-037, BE-E2E-015,
  FE-E2E-017, probe taxonomy sweep (keys, names, limits, filters, concurrent creations).

### 10. Test suites and partial runs (Planning #27, MVP D2)

- **Behavior**: a project defines static suites (a list of test cases) and query suites (tag + classification). CI
  reports a run for a suite with `?suite=<key>`: only the suite's active automated test cases are expected, so a smoke
  run has no untested noise; other results stay valid but outside the universe. The run shows its suite; runs and
  test cases filter by suite. Archived suites take no runs.
- **API**: `GET/POST /projects/{key}/suites`, `GET/PATCH /projects/{key}/suites/{suiteKey}`,
  `PUT /projects/{key}/suites/{suiteKey}/cases`; `suite` query parameter on ingestion, `GET /test-runs` and
  `GET /test-cases` (with `project`); `suite` on the run.
- **Data**: migrations 00021 (`test_suites`, `test_suite_cases` with composite keys to the project; triggers: no
  delete, key/kind/project immutable, members only for static suites) and 00022 (`test_runs.suite_key/suite_name`,
  part of the run's immutable identity).
- **UI**: Suites page (per current project) with creation; suite page with members or matches, add/remove, archive,
  the CI hint and the runs link; suite badge on runs (screenshots 53–55).
- **Tests**: unit (service, handlers, ingestion, execution filter), BE-INT-047, backend and frontend contract,
  FE-INT-038, BE-E2E-016, FE-E2E-018, probe suites sweep.

### 11. Manual execution (MVP D3, Planning #4)

- **Behavior**: a member starts a manual run from the run list (what is tested, project, optional suite, manual or all
  test cases, branch). The run page shows the manual execution panel: Pass / Fail / Blocked / Skip per expected test
  case with a note and the failed step; recording again is a re-test (the last result counts, not flaky). Complete or
  cancel ends it; afterwards it is read-only like any run (summary, verdict, history).
- **API**: `POST /test-runs/manual`, `POST /test-runs/{id}/manual-results`, `POST /test-runs/{id}/finish`; runs have
  `mode` and `startedBy`, results `recordedBy` and `failedStep`; execution status `running`.
- **Data**: migration 00023 (`test_runs.mode/started_by`, `test_results.recorded_by/failed_step`; triggers allow appends
  only to running manual/live runs and freeze a finished run's status).
- **UI**: new manual run page, manual execution panel on the run page, running/manual badges (screenshots 56–57).
- **Tests**: unit (execution service, ingestion orchestration and handlers, DTOs), BE-INT-048, backend and frontend
  contract, FE-INT-039, BE-E2E-017, FE-E2E-019, probe manual sweep (inputs, concurrency, closed runs).

### 12. Requirements and traceability (Incubator: Requirements Federation)

- **Behavior**: a project's requirements are written in Provenly (`R-1`, `R-2`…) or mirrored from Jira, GitHub or
  Azure DevOps by their id there. Members link test cases to them; each requirement shows its coverage from the latest
  result of every covering test case (not covered, not run, failing, partially passing, passing). The test case page
  lists the requirements it covers. Maintainers (or a CI job with their session) push imports; re-imports update
  title, description, link and status in the source and keep links.
- **API**: `GET/POST /projects/{key}/requirements` (`?testCase=` narrows to what a test case covers),
  `POST .../requirements/import`, `GET/PATCH .../requirements/{id}`, `PUT .../requirements/{id}/test-cases`.
- **Data**: migration 00024 (`requirements`, `requirement_test_cases`, `projects.next_requirement_number`; a trigger
  keeps provider and external id immutable and forbids deletes).
- **UI**: Requirements page, requirement page, covered requirements on the test case page (screenshots 58–60).
- **Tests**: unit (catalog service and handlers, coverage rules, `LatestStatuses`, frontend helpers), BE-INT-049 and
  BE-INT-050 (concurrent numbering), backend and frontend contract, FE-INT-040, BE-E2E-018, FE-E2E-020, probe
  requirements sweep (inputs, imports, links, concurrency).
