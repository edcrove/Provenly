# Provenly — full-product exploration prototype

Branch `prototype/full-product` (from `main` at 5245a4c, 2026-10-05). Ed asked for the **whole product** built from
everything written so far (Trello backlog, Notion Incubator, Planning Backlog, Decision Register, product pages),
including ideas that were never refined, **without his intervention**: every open question is decided here, with the
reasoning, so the prototype can be reviewed afterwards and the real backlog refined from it.

Rules kept from `main` (CLAUDE.md): contract first, a goose migration per schema change, tests in every layer touched on
both sides, the 8 + 2 coverage gates at 100%, `make lint` clean. One PR per feature into this branch, self-reviewed.
Nothing here is merged into `main`.

Status legend: ✅ merged into the prototype branch · 🚧 in progress · ⏳ planned.

**Outcome (2026-10-05):** all 21 features are built and merged, each with its own PR, decisions (`Pn-m` below) and
tests in every layer; the full suite (`make coverage`) passes all gates at 100%, the edge-case probe passes on a fresh
stack, and the screenshots in `docs/screenshots` show every UI flow. Paused topics stay paused (MVP topic 7, D8–D13).
For a review, start with the feature map, then the decision log (what was decided without Ed and why), then each
feature's section (behavior, API, code, tests).

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
| 13 | Issues, known issues and issue verification | Incubator, Planning #8 | ✅ | proto/13-issues |
| 14 | Quality dashboard (trends, flaky, coverage) | Incubator (Quality Intelligence) | ✅ | proto/14-dashboard |
| 15 | Live runs: execution sessions, live events, reconciliation | Trello Live Streaming, Planning #9 | ✅ | proto/15-live-runs |
| 16 | Playwright reporter (`@provenly/playwright-reporter`) | Trello, DEC-15 | ✅ | proto/16-playwright-reporter |
| 17 | OpenTelemetry basic instrumentation | Trello, DEC-11 | ✅ | proto/17-otel |
| 18 | Export sink (webhooks) and GitHub connector, secrets at rest | Planning #3, #21, Incubator, MVP D4 | ✅ | proto/18-integrations |
| 19 | MCP server (agent interface) | Incubator, DEC-10 | ✅ | proto/19-mcp |
| 20 | Audit log | Incubator (Project & Authorization) | ✅ | proto/20-audit |
| 21 | Release pipeline, self-hosting guide, dogfooding, public readiness | Trello phase 4 | ✅ | proto/21-release |

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
| P2-4 | Passwords | bcrypt cost 12, at least 10 characters (counted as characters, 2026-10-06) to 72 bytes (bcrypt's limit, never truncated silently); one answer for unknown user and wrong password, with equal timing | NIST-style minimum length, no composition rules; no account enumeration |
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
| P7-1 | Detecting retries | Only when the report says so: Surefire `<flakyFailure>`/`<flakyError>` (failed attempts before a pass) and `<rerunFailure>`/`<rerunError>` (attempts after a failure), or an `attempt` (1-based) / `retry` (0-based, Playwright) testcase property. Repeated names without a signal stay variants: they share their attempt, a failure among them fails the test case and the ingestion warns how many (2026-10-06) | Treating every repeated name as a retry would silently turn failed variants into flaky passes in existing reports |
| P7-2 | Test identity | A test is its suite + class + name within the run; its attempts are numbered 1..100 | Variants (e.g. per browser) have different names and keep aggregating failed > error > skipped > passed (D1) |
| P7-3 | Logical result | The highest attempt of each test (every result of it on ties: variants); a pass after a failed or errored attempt is `passed` and **flaky**, unless the test case failed or errored in the run (2026-10-06) | MVP D1, whatever the cause |
| P7-4 | Storage | Every attempt is stored as a result with `attempt`; `retried` (a later attempt exists) is derived in queries; results stay immutable | Nothing reported is lost; the history shows every attempt |
| P7-5 | Exposure | `flaky` in run outcome and summary (TC-IDs), `flaky` per summary test case, `attempt` and `retried` per result; UI: flaky badge (list and detail), flaky test cases, attempt markers in results and history | Flaky passes count as passed but stay visible |
| P7-6 | Limits | Attempts beyond 100 keep the last 100 with a warning; invalid attempt/retry values are first attempts with a warning; Surefire attempt details come from `<stackTrace>`, their duration is unknown | Broken reporters never fail ingestion |
| P8-1 | Encodings | `Content-Encoding: gzip` (and `x-gzip`); no encoding or `identity` as before; anything else (br, deflate, lists) stays a 415 | D6; gzip is what CI tools produce with one command |
| P8-2 | Size limit | `PROVENLY_MAX_INGEST_BYTES` applies to the decompressed report (413 problem past it) and the compressed body is read through the same limit | D6; a gzip bomb never expands past the limit in memory |
| P8-3 | Broken streams | Not gzip, truncated or corrupt: 400 `invalid_junit` ("body is not valid gzip: …"); **superseded 2026-10-06** (it was `validation_error` on `body`) | An unreadable report, like broken XML; CI scripts branch on one code |
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
| P13-1 | Model | Issues are federated like requirements (native `I-n` or mirrored from Jira / GitHub / Azure DevOps by external id) with a normalized **state** (open / closed) besides the free-text status in the tracker; imports must state it; closing stamps `closedAt` | The DEC-8 matrix needs a two-valued state; trackers' workflows vary, so the mapping to open/closed belongs to whoever pushes the import |
| P13-2 | Verification | Derived on read, never stored, with the DEC-8 base matrix per linked test case (open+fail = known issue, closed+fail = reopen, open+pass = not reproducible, closed+pass = validated fixed; error counts as a failure; no conclusive result = unverified) | Notion 05 Issue Verification State Machine, accepted as DEC-8 |
| P13-3 | Inconclusive | The evidence is the latest **conclusive** logical result (passed / failed / error); a later skipped run keeps it and the link says `latestInconclusive` with the evidence run id | "BLOCKED, SKIPPED… must not replace a previous conclusive verification" (Notion 05) |
| P13-4 | Aggregation | The issue takes its worst link: reopen > known issue > unverified > not reproducible > validated fixed. Not configurable in the prototype | Notion asks for a configurable policy; one safe default first, configuration once there is a second policy to offer |
| P13-5 | Known issues | The run page lists failing test cases linked to an open issue as "known issue" and the rest as "new failures"; the run's verdict is unchanged | Triage help without hiding failures in the verdict |
| P13-6 | Not now | No verification timeline (history), no write-back to trackers (reopen, comment, transition) | Timeline needs stored state changes (feature 20, audit log); write-back needs tracker credentials (feature 18) |
| P14-1 | Scope | A per-project dashboard with the candidate areas of Notion 21 that existing data supports: latest run, pass-rate trend, test cases not executed recently, flaky test cases, automation, requirement coverage, issue verification. Release/environment quality and duration trends are left out | Releases and environments are not modelled yet; durations are optional in JUnit and too sparse to trend |
| P14-2 | Automation | Automation rate = automated / active test cases, from the catalog flag (no AutomationMapping) | Notion 21: "do not define automation coverage via AutomationMapping"; the denominator is explicit and stable |
| P14-3 | Stale | An active test case is stale when its latest valid result is older than `staleDays` (default 14, 1–365) or it has none; never-executed ones are listed first, then the oldest (20 listed, counts complete) | Highlights tests nobody runs; the threshold depends on the team's cadence, so it is a parameter |
| P14-4 | Flaky | Counted per test case over the latest `window` runs (default 20, 1–200) with the D1 rule (passed on its last attempt after a failed or errored one); manual re-tests never count | Same definition as the run summaries; a window keeps old flakiness from dominating |
| P14-5 | Architecture | A new read-only `insights` module computes what no module owns (cross catalog/execution); the trend, coverage and verification come from the existing endpoints | Keeps modules owning their data; one new endpoint instead of a monolithic dashboard API |
| P15-1 | Lifecycle | CI starts a live run (`POST /test-runs/live`, API key or member session, same provider/runId/runAttempt identity as reports; a retried start returns the run); it is `running` until its JUnit report arrives through `/ingestion/junit`, which completes it with the report's results and status | The card's "TestRun created at the start"; the final report stays the source of truth and the existing ingestion keeps working unchanged for batch CI |
| P15-2 | Events | `POST /test-runs/{id}/events` takes batches of up to 500 events (test.started/finished, step.started/completed, run.finished) with eventId, sequence, timestamp, test name and optional TC-ID; repeated event ids are counted as duplicates and skipped; at most 10,000 per run; refused once the run finished | Idempotent delivery lets runners retry freely; bounds keep a broken runner from filling the database |
| P15-3 | Transport | HTTP batches in, polling (every 2 s) out; no WebSocket and no River queue in the prototype | The card names WebSocket and River as options; polling is enough to prove the model, keeps the stack unchanged and loses nothing if the channel drops (events are persisted) |
| P15-4 | Live state | Derived on read: per expected test case waiting / running / its last finished status (by sequence); counts and the runner's run.finished flag; provisional only | "Live state is provisional; the final suite result is the source of truth" |
| P15-5 | Reconciliation | Computed on read once the run is finished: CONSISTENT or MISMATCH with the card's kinds (status mismatch, live-only, final-only, started-without-finished, duplicate, invalid correlation). Events carry the test's attempt: each test (by name) counts with its last finished attempt and a test case's variants aggregate failed > error > skipped > passed, then compare with the run summary per TC-ID; a duplicate is one attempt of one test finished twice (a retry is not); events without a TC-ID are not reconciled | A mismatch never changes the final results; per-TC-ID comparison matches how summaries count |
| P15-6 | Not now | No execution-session concept beyond the run, no step-level results, no automatic timeout of a run whose report never arrives (a member can still see it as running) | Kept for the reporter (feature 16) and a later decision on abandoned runs |
| P16-1 | Shape | A dependency-free TypeScript reporter in `reporters/playwright` (Playwright types declared structurally): live run at `onBegin`, `test.started`/`test.finished` events (with attempt) batched by 50, `run.finished` and the final JUnit report at `onEnd` | Works with any Playwright that has the Reporter API; nothing to keep in sync with Playwright releases |
| P16-2 | TC-ID | A test declares its TC-ID with a `tc-id` annotation, a `@KEY-n` tag or a KEY-n in its title (that order) | Uses Playwright's own metadata first; the title form matches the JUnit name fallback |
| P16-3 | Report | One JUnit testcase per attempt with `tc-id` and `attempt` properties; Playwright `interrupted` is an error, `timedOut` a failure; an interrupted or timed-out run is sent with `status=interrupted` | Reuses the ingestion's retry model (D1) and execution statuses unchanged |
| P16-4 | Resilience | Provenly unreachable never fails the test run: a failed live start or event batch is logged and the final report is still sent (3 tries on network errors and 5xx; 4xx is logged, not retried) | Reporting must not turn a green build red |
| P16-5 | Configuration | `PROVENLY_URL`, `PROVENLY_API_KEY`, optional `PROVENLY_PROJECT`/`PROVENLY_SUITE`; run identity from `GITHUB_*` on GitHub Actions, else `local` and a timestamp; options override all; no URL means no-op | Zero configuration in CI beyond the key; local runs stay silent |
| P16-6 | Quality and dogfooding | Own CI job with 100% statement/branch/function/line thresholds; BE-E2E-022 drives it against the real API; Provenly's own E2E journeys load it (inactive unless `PROVENLY_URL` is set). Publishing to npm is left to feature 21 | Same bar as the product; dogfooding ready without coupling CI to a running instance |
| P17-1 | Spans | The OTel SDK is always on: every request is a span named after its route (`GET /api/v1/test-runs/{testRunId}`, method only when unrouted), every database query a child span (otelpgx), and JUnit ingestion a span with run id, created flag and result count plus a parse span; W3C `traceparent` is continued | The card's "requests, ingestion and DB access produce useful spans"; route names keep cardinality bounded |
| P17-2 | Export | Spans leave the process only when `OTEL_EXPORTER_OTLP_ENDPOINT` (or the traces-specific variable) is set, over OTLP/HTTP with the standard `OTEL_EXPORTER_OTLP_*` settings; a bad exporter configuration stops startup | No collector needed for the demo; standard variables work with any backend (Jaeger, Tempo, Honeycomb…) |
| P17-3 | Logs | Every log record written in a request carries `trace_id` and `span_id` (slog handler); every response carries `X-Trace-Id` | Logs and traces correlate; a user can quote the trace id in a bug report |
| P17-4 | Not now | No metrics, no test result ↔ trace link (the ingestion's trace id is in its logs; a `trace_id` column can come later), no collector in docker compose | Card: "leave room to associate traceId/spanId to TestResult later"; metrics need a decision on what to measure |
| P18-1 | Secrets at rest | Secrets Provenly must read back (webhook signing secrets, GitHub tokens) are encrypted with AES-256-GCM under `PROVENLY_SECRETS_KEY` (32 bytes, base64, required in prod; random with a warning elsewhere), stored versioned (`v1:`); the database refuses clear values. A secret is shown once (webhook) or only as its last 4 characters (token) | Closes MVP D4 and P4-6; the version prefix leaves room for key rotation |
| P18-2 | Webhooks | Maintainers subscribe https endpoints to `run.completed` (every completed CI, live or manual run, with the run as `getTestRun` returns it); deliveries are rows (outbox) sent by an in-process worker every 2 s, signed `sha256=HMAC(secret, "<timestamp>.<body>")`, retried 10 s / 1 min / 5 min / 30 min then failed; paused webhooks get nothing new; `ping` checks an endpoint | Planning #3 export sink; an outbox survives restarts and never blocks ingestion; the Stripe-like signature is familiar |
| P18-3 | SSRF | Outbound calls refuse private, loopback and link-local addresses after DNS resolution, follow no redirects and ignore proxies; plain http and private targets are allowed only where `PROVENLY_WEBHOOKS_ALLOW_PRIVATE` (default: all environments but prod) | Notion 17 threat model: a webhook URL must not reach the deployment's own network |
| P18-4 | GitHub connector | One repository per project, a token (fine-grained, issues read) and optional labels; "Sync" mirrors up to 500 issues (no pull requests) into the project's issues by number with their open/closed state, through the issue import of feature 13; failures are 502 and kept on the connection; disconnecting keeps the mirrored issues | Planning #21; read-only sync, manual trigger, reuses DEC-8 verification |
| P18-5 | UI | Webhooks and GitHub Issues are sections of the project page, for maintainers (like API keys) | Project-level configuration lives in one place |
| P18-6 | Not now | No scheduled GitHub sync or GitHub webhooks in, no other events (run.started, issue changes), no Jira connector, no key rotation command, no delivery replay button | Kept small; the outbox and secrets box are the foundations for them |
| P19-1 | Transport | MCP over Streamable HTTP at `POST /api/v1/mcp` (protocol 2025-06-18, also 2025-03-26), one JSON-RPC message per request, JSON responses (no SSE stream, no batches); notifications are 202. No SDK dependency: the subset (initialize, ping, tools/list, tools/call) is small | Works with Claude Code and other HTTP MCP clients; nothing to deploy beside the API |
| P19-2 | Tools as API calls | Each tool is a read-only GET to the public API made in-process with the caller's own credentials; results are the API's JSON (`structuredContent` and text), API errors are tool results with `isError` and the problem text | One authorization model and one set of shapes (the contract); agents can never see more than the user through the API |
| P19-3 | Tools | 13 read tools: projects, test case search/detail/steps/history, runs, run detail/summary/results/live, project quality, issues, requirements. Arguments are validated (types, positive integers, key-shaped path segments, no unknown arguments) before any call | Covers "what is failing, since when, is it known" questions; write tools wait for a decision on agent writes |
| P19-4 | Authentication | Agents use a session token (`POST /auth/login`, 12 h); the account page shows the commands to connect Claude Code | No new credential type in the prototype; API keys stay CI-only (P4) |
| P19-5 | Not now | No write tools, no resources or prompts, no personal access tokens, no SSE notifications | DEC-10 keeps v0 read-only; tokens and writes need Ed's decision on agent permissions |
| P20-1 | What is audited | Every successful authenticated change made through the API (POST, PUT, PATCH, DELETE with a 2xx): actor (username, or API key by prefix and name), action (method and route), path, project key (path or `?project=`), status, time. Reads, MCP calls and the live event stream are not; sign-in and accepting an invitation (no actor yet) are not | One mechanism covers every module, including future ones, with no per-handler code |
| P20-2 | No bodies | Request and response bodies are never recorded | They can carry secrets (tokens, passwords, webhook secrets); the route and path say what changed |
| P20-3 | Mechanism | An audit router wraps each handler inside authentication, so the caller is known; the event is written after the handler answers and a failure to write is logged, never turned into an error | The change already happened; failing it afterwards would lie to the client |
| P20-4 | Storage and access | Table `audit_events`, append-only (a trigger refuses updates and deletes); `GET /api/v1/audit` for administrators, newest first, filtered by project and actor; an "Audit" page in the header for administrators | Tamper-evident enough for the prototype; project maintainers' view can come later |
| P20-5 | Not now | No before/after values, no retention policy or export, no audit of reads or sign-ins, no per-project audit for maintainers | Need decisions on retention and privacy |
| P21-1 | Releases | A `vX.Y.Z` tag on a commit whose CI is green publishes the API and web images to GHCR (version and `latest`), the Playwright reporter to npm (with provenance, only when the `NPM_TOKEN` secret exists) and a GitHub release with generated notes; the version is stamped into the binary and logged at startup | One action (a tag) ships everything; nothing publishes from an untested commit |
| P21-2 | Reporter package | `@provenly/playwright-reporter` builds to `dist/` (JS + types); CI checks what `npm publish` would ship | Consumers get JavaScript, not TypeScript sources |
| P21-3 | Self-hosting | `docs/self-hosting.md`: the prod compose environment with released images (`IMAGE_PREFIX`/`IMAGE_TAG`), required secrets, TLS proxy, backups, upgrades and operations; `SECURITY.md` for private reports | What a team needs to run it without reading the code |
| P21-4 | Sign-in throttle | Five failed sign-ins of a username within 15 minutes lock it (429, with how long) even with the right password; a success clears it; unknown usernames are throttled alike; in memory, bounded to 10 000 names | Slows password guessing per account without new infrastructure; per-address limits belong to the reverse proxy (documented) |
| P21-5 | Dogfooding | CI's E2E journeys report themselves to a Provenly instance through the reporter when the `PROVENLY_URL`/`PROVENLY_API_KEY` secrets are set | Provenly tracks its own tests as soon as an instance exists, with no CI change |
| P21-6 | Not now | No Helm chart or multi-instance throttle (shared store), no signed images (cosign), no SBOM, no automatic changelog beyond generated notes | Single-host prototype |
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

### 25. Refined follow-ups (2026-10-06)

The audit's open decisions and cards were refined by the product owner and the six personas and recorded in the
Notion Decision Register; each one ships in its own PR.

- **Stale by execution date (card #52):** "Not executed recently" counts from the start of a test case's latest run
  (`startedAt`, the upload time when the report gave none), so a late upload of an old run does not make it look fresh.
- **Find by key (card #53):** `GET /api/v1/test-cases?key=CHK-12` gives zero or one test case (MCP `search_test_cases`
  takes `key` too); the test case list has a TC-ID search box.
- **Cheaper run reads (card #44, S):** routes on a run authorize it with its project alone (`GetRunProject`); only
  `GET /test-runs/{id}` loads the run and its outcome, and a live poll reads the run and its summary inputs once
  (five statements whatever the run's size, BE-INT-066). Persisted counters (M) stay as an option if needed.
- **Variants and flaky (card #58):** repeated names without an attempt signal are variants of the test (a failure
  among them fails the test case; the ingestion warns how many), and a test case that failed or errored in a run is
  not flaky there, in the summary, the dashboard ranking and the latest status alike.
- **Broken gzip is an unreadable report:** a body sent as gzip that is not gzip, truncated or corrupt answers
  `400 invalid_junit` ("body is not valid gzip: …"), like broken XML (replaces P8-3, which said `validation_error`).
- **Sharded runs (card #57):** one logical CI run may arrive as N reports (`?shard=i/N`; the Playwright reporter reads
  `--shard`): the first creates a running run, each shard is taken once, the last completes it (webhook once) and
  `POST /api/v1/ingestion/finalize` ends one whose shards will not all arrive as interrupted, naming the missing ones.
  Results carry their shard (filter `?shard=`); the run page shows "2 of 3 shards received" (screenshot 69). A GitHub
  re-run of only the failed shard jobs (attempt N inheriting the shards of N-1) goes to the Incubator.
- **Supply chain (card #50):** every GitHub Action is pinned to a commit SHA with its version as a comment
  (`scripts/docs-check.sh` fails on an unpinned `uses:`), and Dependabot proposes updates for actions, Go modules,
  npm packages and Docker images.
- **Passwords (card #60):** the minimum counts characters, not bytes (`contraseña` is 10); the maximum stays 72 bytes and the
  forms and the error say non-ASCII letters count as 2 to 4. Same rule for `PROVENLY_ADMIN_PASSWORD`.

### 24. Review-panel audit (2026-10-06): fixes

The full panel (`review-panel`: 6 technical experts, architect, product owner, 6 coverage audits, 6 personas) ran on
the whole product. Confirmed defects are fixed in small PRs, each with tests that fail without the fix; product
questions went to Ed.

**Security**
- The sign-in throttle reserves each attempt before the password check (parallel guesses no longer all pass) and,
  when its map is full, drops the names with the fewest failures instead of starting over (a flood of made-up names
  no longer unlocks a locked one). Infrastructure errors do not count as failures.
- The session cookie is `Secure` behind the documented TLS proxy: nginx passes `X-Forwarded-Proto` through instead
  of replacing it with its own `http`. The web container sends CSP, frame, `nosniff` and referrer headers.
- `PROVENLY_ENV` accepts only known names (an unknown one silently got development defaults: random keys, SSRF guard
  off, demo password accepted); prod blocking private targets by default is now tested.
- The SSRF guard also refuses "this network", CGNAT (Alibaba metadata), benchmarking, documentation, reserved and
  broadcast ranges, Azure WireServer, Teredo, and IPv4-mapped / NAT64 / 6to4 forms of blocked addresses.
- GitHub connector: repository segments `.`/`..` are refused (service and database, migration 00030), and pointing
  the stored token at another repository needs the token again.
- Members and API-key routes answer an invisible project exactly like an unknown one (no internal id).
- Accepting an invitation hashes the password only after the token is found (made-up tokens cost no bcrypt).
- Invitation links carry the token in the URL fragment (never sent to a server or logged); older `?token=` links
  still work.
- The HTTP server cuts off clients that trickle a request (read and idle timeouts).

**Database: concurrency and performance**
- Latest-results lookups (requirement coverage, issue verification) walk the test case's own runs through a new
  partial index (migration 00031) instead of scanning results backwards or aggregating whole histories: a stale
  test case went from minutes to milliseconds with long history (BE-INT-058).
- Replacing a requirement's, issue's or static suite's test cases, and creating dimensions and values, are
  serialized per scope (transaction advisory lock): no union of two concurrent lists, no limit overrun, no repeated
  value positions (BE-INT-059, BE-INT-060).
- Webhook deliveries are leased for 5 minutes (longer than a worker pass) and an attempt is recorded only if the row
  is still the claimed one: a late worker can no longer turn a succeeded delivery back to pending (BE-INT-055).
- The 101st manual record of a test case is a 409 conflict, not a database error (BE-INT-048).
- Run list and detail read every run column through `sqlc.embed` (the list dropped the report digest; same class
  as F12).

**API contract and probe**
- Webhook and GitHub routes validate `{projectKey}`, member routes validate `{username}`, before any lookup: a NUL or
  invalid UTF-8 byte was a 500; it is a 400 on every route (the spec lists it).
- The edge-case probe also runs against the API port in CI (nginx's limits hid the API's own), signs in for the
  oversized upload, and sweeps malformed keys and usernames and the paging of users, invitations, members and API
  keys.
- The Playwright reporter clips live event names to 1000 characters and drops an over-long TC-ID: one long title
  used to make the API reject the whole event batch.

**Frontend**
- A malformed requirement or issue id is "Page not found" at once (it loaded forever).
- New test case and new manual run send the project the select shows: with no current project, or one the user
  cannot write to, they used to send a hidden `TC`.
- "Runs of this suite" carries the project (suite keys are per project) and the runs list honours it.
- A failed "Mark as automated" is shown; the dashboard keeps its figures and focus while a filter changes; the run
  trend is a list of links that say their verdict (provisional while running); the latest-run card tells loading, a
  failed read and no runs apart.
- A running run's pass rate says "so far" in the header, summary and list, and its summary polls while it runs.
- Empty lists name the filter that emptied them (runs, issues, audit).
- Project page sections no longer set the tab title (a failed key list turned it into "Error").
- Table cells are never flex or grid containers (guarded by a unit test); notices use `role=status`, errors
  `role=alert`.
- Manual execution: a typed failed step disables Pass and Skip instead of being dropped; the TC-ID link says it opens
  a new tab.
- A long or multi-line failure message without details opens in full; the invitation form marks and explains each
  refused field.

**Docs and release**
- `release.yml` grants no permission by default (each job asks for its own) and `latest` follows the highest version
  only; no workflow leaves the checkout token on disk (`persist-credentials: false`).
- The reporter is not on npm yet (the `@provenly` scope is not registered to this project): the docs install it from a
  checkout, say how to run it outside GitHub Actions and that a TC-ID must exist first.
- `scripts/docs-check.sh` (in `make lint` and CI) keeps the screenshot index, the reporter install instructions and
  the workflow permissions honest.
- The README describes the full-product prototype; `docs/architecture.md` lists all eight modules and the one
  cross-module foreign-key exception; compose passes the standard OpenTelemetry variables; self-hosting says no
  image is published yet, to deploy from the release tag's checkout and requires Compose v2.24+.

**Test fidelity**
- The frontend mock behaves like the server where tests relied on it: a manual run's and an amended run's outcome
  are recomputed from their results, a skipped latest result keeps the previous conclusive evidence of an issue, the
  quality endpoint lists stale test cases with their last execution, and ids are never handed out twice.
- BE-INT-001 compares the whole schema (columns, constraints, indexes, functions, triggers, sequences, types)
  before a reset and after migrating up again, and asserts the reset leaves nothing behind.
- New contract checks: a deprecated test case stays editable (implementation decision #10), gzip reports replay like
  plain ones, CI API keys get 401 on manual-run routes, scoped lists return the visible items and page over them.
- No wall-clock tolerances: the deep-page check uses medians and a relative bound; backoff and stale dates are exact.
- covgate fails a gate when a test carries an id its inventory does not declare (BE-INT-020 was missing).

**Domain**
- Manual and live runs record when they started (the run page and history said "Started —").
- A live run's reconciliation counts final results outside the run's universe (a manual or deprecated test case CI
  ran): their live events no longer read as `live_only` mismatches.
- A suite run explains results outside its universe by the suite ("outside suite Release, or not automated"), not
  only by automation.

### 23. Review agents (technical experts and user personas)

- **What**: fourteen read-only subagents in `.claude/agents/` — technical experts (`reviewer-api-contract`,
  `reviewer-database`, `reviewer-security`, `reviewer-tests`, `reviewer-frontend`, `reviewer-domain`) that review a
  diff against the repo's rules and return findings with severity and `path:line`, and user personas
  (`persona-qa-lead`, `persona-developer`, `persona-sdet`, `persona-manual-tester`, `persona-devops`, `persona-engineering-manager`)
  that walk their journeys over the screenshots or a running stack and report friction.
- **Feature coverage audit**: `reviewer-tests` also runs per feature — it derives the acceptance criteria from the
  card, decisions and contract, plans every test from zero (before reading the existing ones), then maps the plan
  to the tests that exist and reports a traceability matrix with covered, partial and missing conditions.
- **Whole-project reviewers**: `architect` reviews the codebase as a whole (module boundaries, layering,
  consistency, data access, runtime, evolvability) and proposes incremental refactors; `product-owner` checks the
  product against Ed's vision and decisions (Notion, Trello, docs, screenshots), reports drift and gaps and
  proposes future ideas for the Incubator — Ed decides.
- **How**: the `review-panel` skill picks the agents by what the diff touches, runs them in parallel, verifies every
  blocker and major, fixes confirmed defects with regression tests and sends product questions to Ed. The checklists
  encode lessons from earlier rounds (e.g. audit F12: hand-mapped query columns, mocks hiding backend gaps).

### 22. Full-product audit (screenshots of every flow, then code)

Every screenshot of the flow was reviewed and the new modules re-read; each finding was fixed with a regression test
that fails without the fix.

- **F1** A running run showed its verdict as if final: the badge now reads "<verdict> so far" with a tooltip while
  the run is running (run page, runs list, dashboard).
- **F2/F10** A share of nothing read 0%: with no executed test case every "% of executed" (and the pass rate) is
  "—", and with no expected test case every "% of expected" and the execution % is "—".
- **F3** An unfiltered run without results said "No results match the filters"; it now says "No results yet.".
- **F4** Issue and requirement pages ran the tracker link and "All issues/requirements" together; they are separated.
- **F5** The project page (members, classification, API keys, webhooks, GitHub) was titled "<KEY> members"; it is
  now "Project <KEY>" with a "Members" section.
- **F6** A finished live panel still counted tests as "running"; it now reads "As streamed live: … started but never finished ·
  … never started".
- **F7/F8** The runs-list breakdown and mobile dates broke mid-phrase; they wrap only between parts.
- **F9** The GitHub token hint showed the last four characters of any token, the whole of a short one; tokens under
  12 characters show only "…".
- **F11** A manual test case with results from manual runs warned "Receives automated results but is marked
  manual"; only results of batch or live runs warn now.
- **F12** The test case history returned its runs without their mode, suite, starter and report digest (every run
  read as a batch run without suite); the history query now selects them, so the history matches the run itself.
- **Tests**: FE-INT-048 (new), FE-INT-014, FE-INT-009, FE-INT-043, FE-INT-044, FE-E2E-023 (provisional badge and
  finished live panel), FE-E2E-019 (no warning after a manual run), BE-INT-047/048 (history carries suite, mode and
  starter), integrations unit tests (short-token hint).
- **Kept as is** (judged correct): mobile tables scroll horizontally; requirements without linked test cases read
  "0/0 passing"; coverage counts results of running runs (they are the latest known status).

### 21. Release, self-hosting, dogfooding, public readiness (Trello phase 4)

- **Behavior**: tagging `vX.Y.Z` publishes images, the reporter and a release; teams self-host with released images
  following `docs/self-hosting.md`; repeated failed sign-ins lock a username for 15 minutes; Provenly's own E2E
  journeys can report to a Provenly instance.
- **Code**: `.github/workflows/release.yml`, CI reporter packaging check and dogfooding secrets, `app.Version`
  (ldflags, startup log), compose `IMAGE_PREFIX`/`IMAGE_TAG`, reporter `dist/` build, identity sign-in throttle
  (`throttle.go`, `apperr.TooManyRequests` → 429), `SECURITY.md`, `docs/self-hosting.md`.
- **Tests**: unit at 100% (throttle: lock, case-insensitive, window end, success clears, unknown names, bound; 429
  mapping; startup log), backend contract (429 problem), frontend contract (429 scenario) and FE-INT-047, BE-E2E-027,
  probe throttle check (the sweep no longer fails sign-ins as the administrator); workflows checked with actionlint.

### 20. Audit log (Incubator, Project & Authorization)

- **Behavior**: every change made through the UI or the API is recorded with who made it (a user or a CI API key),
  the operation and the project; administrators read it on the Audit page (screenshot 68), filtered by project and
  actor.
- **API**: `GET /api/v1/audit?project=&actor=&page=` (administrators).
- **Code**: module `internal/audit` (+ `postgres`, `auditdb`), migration 00029 (append-only trigger), audit router in
  `internal/app` around the session and API key routes; frontend `features/audit/AuditPage.tsx`, "Audit" header link.
- **Tests**: unit at 100% (which routes are audited, failures skipped, key actors, storable text, validation, admin
  only, handler), BE-INT-057 (through the API on a real database, concurrency, filters, append-only and constraints),
  backend and frontend contract, FE-INT-046, BE-E2E-026, FE-E2E-026, probe audit sweep.

### 19. MCP server (Incubator, DEC-10)

- **Behavior**: an MCP client (e.g. `claude mcp add --transport http provenly <url>/api/v1/mcp --header "Authorization:
  Bearer <token>"`) gets 13 read-only tools over Provenly's data, seeing exactly what its user sees. The account page
  shows the commands (screenshot 40).
- **Code**: `internal/mcp` (JSON-RPC, tool table, in-process API calls), mounted by `internal/app` on the session
  routes; `api/openapi.yaml` documents `POST /api/v1/mcp`; frontend `lib/mcpSnippet.ts` and the account page card.
- **Tests**: unit at 100% (protocol messages, negotiation, errors, argument validation, credentials passed through,
  API errors as tool errors), backend contract (initialize, notification, tools over a real database, a user without
  access gets a tool error, 413/415, 401 via the sweep), FE-INT-045, snippet unit test, BE-E2E-025, FE-E2E-025, probe
  MCP sweep.

### 18. Integrations: webhooks and GitHub Issues (Planning #3, #21, MVP D4)

- **Behavior**: on the project page a maintainer adds a webhook (the signing secret is shown once), pings it, pauses
  it and reads its deliveries; every completed run is POSTed signed and retried. GitHub Issues: connect a repository
  with a token (shown only as `…abcd`), sync to mirror its issues into the project's issues, see a failed sync's
  error, rotate the token, disconnect.
- **API**: `GET/POST /projects/{key}/webhooks`, `PATCH /webhooks/{id}`, `POST …/ping`, `GET …/deliveries`,
  `GET/PUT/DELETE /projects/{key}/github`, `POST /projects/{key}/github/sync` (502 `upstream_error`, 409 unreadable token).
- **Code**: module `internal/integrations` (+ `postgres`, `integrationsdb`), `platform/secrets`, migration 00028,
  ingestion `RunNotifier`, delivery worker in `serve`, `PROVENLY_SECRETS_KEY` / `PROVENLY_WEBHOOKS_ALLOW_PRIVATE` /
  `PROVENLY_GITHUB_API_URL`; frontend `WebhooksSection`, `GitHubSection` (screenshot 67).
- **Tests**: unit at 100% (validation, SSRF guard and redirects, signing, retries and backoff, paused and undecryptable
  secrets, worker loop, GitHub paging/labels/pull requests/failures, handlers, notifier, CLI key warning), BE-INT-055
  (outbox from batch, live and manual runs, signatures, SKIP LOCKED with concurrent workers, lease, DB constraints),
  BE-INT-056 (GitHub mirror, encrypted token, another key), backend and frontend contract (502/409 included),
  FE-INT-044, BE-E2E-024 and FE-E2E-024 (local receiver verifying the HMAC, GitHub double), probe integrations sweep.

### 17. OpenTelemetry basic instrumentation (DEC-11)

- **Behavior**: requests, ingestion and database queries are traced; logs carry trace and span ids; responses carry
  `X-Trace-Id` and continue a caller's `traceparent`. Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export the spans.
- **Code**: `internal/platform/telemetry` (setup, middleware, log handler), otelpgx on the pool, ingestion spans,
  `OTEL_EXPORTER_OTLP_ENDPOINT` passed through docker compose.
- **Tests**: unit (setup with and without export, route-named spans, propagation, log correlation, CLI startup with an
  exporter and with a broken one), BE-INT-054 (ingestion and its queries are one trace), contract (X-Trace-Id and
  propagation), BE-E2E-023, probe tracing sweep (through the proxy, malformed traceparents).

### 16. Playwright reporter (DEC-15)

- **Behavior**: add `['@provenly/playwright-reporter']` to `playwright.config.ts` and set `PROVENLY_URL` and
  `PROVENLY_API_KEY` in CI: each run appears in Provenly as a live run while it executes and is completed by the
  reporter's JUnit report (retries as attempts, flaky detection, TC-IDs from annotations, tags or titles).
- **Code**: `reporters/playwright` (README, unit tests at 100%), CI job "Playwright reporter", `make test-reporter`.
- **Tests**: reporter unit tests (TC-ID sources, status mapping, XML, live streaming, batching, failures and retries,
  no-op without URL), BE-E2E-022 against the real API with a project API key.

### 15. Live runs and reconciliation (Trello Live Streaming, Planning #9)

- **Behavior**: CI starts a live run before executing, streams test events while it runs and finally sends its JUnit
  report as usual. The run page shows live progress (finished / running / waiting and each test case's state,
  refreshed every two seconds); when the report arrives the page reloads as a normal completed run and shows the
  reconciliation of the live events with the final results.
- **API**: `POST /test-runs/live`, `POST /test-runs/{id}/events` (API keys or sessions), `GET /test-runs/{id}/live`.
- **Data**: migrations 00026–00027 (`test_run_events` with the attempt of each event; the identity trigger lets a running live run set its report digest
  once).
- **UI**: live panel on the run page (screenshots 65–66).
- **Tests**: unit (live state, reconciliation, events, completion, handlers, ParseRef), BE-INT-053, backend and
  frontend contract, FE-INT-043, BE-E2E-021, FE-E2E-023, probe live sweep (inputs, idempotency under concurrency,
  completion).

### 14. Quality dashboard (Notion 21 Dashboards, Metrics & Quality Intelligence)

- **Behavior**: the Dashboard (first in the navigation) shows, for the current project, the latest run and its verdict,
  a pass-rate bar per recent run (red when it did not pass, each linking to its run), the automation rate, the test
  cases never or not recently executed (period selectable), the flaky test cases (window selectable), the requirement
  coverage breakdown (archived requirements excluded) and the issue verification breakdown.
- **API**: `GET /projects/{key}/quality?staleDays=&window=` (viewers and up).
- **Data**: none new; two execution queries (`ListLastExecuted`, `ListFlakyCounts`).
- **UI**: Dashboard page (screenshot 64).
- **Tests**: unit (insights service and handler, execution reads), BE-INT-052, backend and frontend contract,
  FE-INT-042, BE-E2E-020, FE-E2E-022, probe quality sweep.

### 13. Issues, known issues and verification (DEC-8, Planning #8)

- **Behavior**: issues are reported in Provenly (`I-1`…) or mirrored from a tracker with their state. Members link the
  test cases that reproduce an issue; each link shows its evidence (latest conclusive result and its run) and its
  verification, and the issue shows the worst one. Closing or reopening (in Provenly or by a re-import) re-derives it.
  Run pages split failures into known issues and new failures; test case pages list their issues.
- **API**: `GET/POST /projects/{key}/issues` (`?testCase=`, `?state=`), `POST .../issues/import`,
  `GET/PATCH .../issues/{id}`, `PUT .../issues/{id}/test-cases`.
- **Data**: migration 00025 (`issues`, `issue_test_cases`, `projects.next_issue_number`; state/closedAt consistency,
  immutable identity, no deletes).
- **UI**: Issues page with a state filter, issue page, linked issues on the test case page, known issues on the run
  page (screenshots 61–63).
- **Tests**: unit (matrix, aggregate, service, handlers, `LatestConclusive`), BE-INT-051, backend and frontend contract,
  FE-INT-041, BE-E2E-019, FE-E2E-021, probe issues sweep.

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
