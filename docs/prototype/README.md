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
| 5 | Optimistic locking (ETag / If-Match) | MVP D7 | ⏳ | |
| 6 | Snapshot amendment per run | DEC-42 | ⏳ | |
| 7 | Retries, logical result and flaky | MVP D1 | ⏳ | |
| 8 | Compressed (gzip) report ingestion | MVP D6 | ⏳ | |
| 9 | Taxonomy: tags and custom dimensions | Planning #26 | ⏳ | |
| 10 | Test suites (static and query) and partial-run scope | Planning #27, MVP D2, Incubator | ⏳ | |
| 11 | Manual execution (manual runs, step results) | MVP D3, Planning #4, Incubator | ⏳ | |
| 12 | Requirements and requirement ↔ test traceability | Incubator (Requirements Federation) | ⏳ | |
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

