# MVP plan (proposal, 2026-10-01)

Status: **accepted plan; decisions D1–D5 taken by Ed on 2026-10-02 (§4)**. Notion stays canonical for *why* (decisions), Trello for execution (cards,
order, validation), this file for the plan as a whole. Nothing here is decided until Ed accepts it; open decisions are
recorded in §4.

## 1. Goal

The POC proved the core loop locally: a stable `TC-<id>`, CI ingestion of JUnit, an immutable expected-universe
snapshot, an honest summary and history, with every test layer at 100%.

**The MVP makes Provenly usable by one real team, every day, on a server they own:**

1. **Secure to expose** — CI ingests with an API key, people log in, nothing secret leaks.
2. **Live and trustworthy runs** — a native Playwright reporter streams the run while it executes; the final report
   stays authoritative and is reconciled against the live stream.
3. **Correct results for real suites** — retries/flaky tests and shards no longer distort the summary (partial
   runs move to the Test Suite design, D2).
4. **Operable** — published images, a documented self-hosted deployment (TLS, backups, upgrades), OpenTelemetry.
5. **Proven on itself** — Provenly's own CI reports to a Provenly instance (dogfooding).
6. **Ready for contributors** — the repository (already public as `edcrove/Provenly`, Apache-2.0) passes the Public
   Readiness Gate.

Out of scope (Post-MVP, unchanged): multi-tenancy, OAuth/SSO, connectors (GitHub/Jira, Notion #3), Issue
Verification (#8), ExportSink (#21), taxonomy and Smart Suites (#26/#27), MCP (#22), AI journeys (#23).

## 2. Exit criteria (MVP Definition of Done)

- A team deploys Provenly from published images with the self-hosting guide, behind TLS, with automated backups.
- Their Playwright suite reports live (with retries and shards) and via JUnit for any other framework.
- Every API route except `/healthz` and `/readyz` requires authentication; ingestion uses an API key.
- Provenly's own CI publishes its runs to a Provenly instance; the history is visible.
- Traces and correlated structured logs exist for API requests and ingestion.
- The 8 gates + 2 consolidated gates stay at 100%; CI is green and required on `main` (ruleset).
- The repository passes the Public Readiness Gate.

Every MVP card also follows the standard card DoD: OpenAPI first, migrations, tests in every layer it touches on both
sides, gates at 100%, docs and screenshots updated, manual validation by Claude, Hecho moved by Ed.

## 3. Phases and order

Sizes: S ≈ 1 day, M ≈ 2–4 days, L ≈ a week or more (Claude implementation time, before validation).

### Phase 0 — Close the POC (done 2026-10-04)

| # | Item | Owner | Depends on |
|---|---|---|---|
| 0.1 | ~~Validate the POC cards in *Validation*~~ — done 2026-10-04: all 22 in *Hecho* | Ed + Claude | — |
| 0.2 | ~~`main` ruleset~~ — done: `main` is protected | Ed | — |
| 0.3 | ~~Merge PR #1, set `main` as default branch~~ — done 2026-10-04 (merge commit 413e2f9) | Ed | 0.1, 0.2 |
| 0.4 | ~~Close POC in Notion~~ — done 2026-10-04 (consolidated page, Decision Register) | Claude | 0.3 |

### Phase 1 — Foundations (parallel, low risk)

| # | Card | Size | Depends on |
|---|---|---|---|
| 1.1 | Toolchain Major Upgrades (TS 7, MSW 3, Node 24) | M | 0.3 |
| 1.2 | OpenTelemetry Basic Instrumentation | M | 0.3 |
| 1.3 | Snapshot Amendment per Run (DEC-42) | M | 0.3 |
| 1.4 | ~~Decisions D1–D5 (§4)~~ — done 2026-10-02, in the Decision Register | Ed | — |
| 1.5 | **Optimistic Locking for Test Case Edits (ETag / If-Match)** (new card; Ed, 2026-10-04, D7) | M | — |

1.3 is the last POC follow-up (already decided); doing it early keeps the summary model stable before live runs
change ingestion.

### Phase 2 — Secure by default

| # | Card | Size | Depends on |
|---|---|---|---|
| 2.1 | Secrets Abstraction | M | — (D4 accepted) |
| 2.2 | Authentication (API Key + JWT) | L | 2.1 |
| 2.3 | Login UI | M | 2.2 |

### Phase 3 — Live, correct runs (the MVP's product value)

| # | Card | Size | Depends on |
|---|---|---|---|
| 3.1 | **Retries & Logical Result** (new card) | M | — (D1 accepted) |
| 3.2 | Live Test Run Streaming & Reconciliation (incl. Execution Session protocol #9, shards) | L | 2.2, 3.1 |
| 3.3 | **Playwright Reporter (first native reporter)** (new card) | L | 3.2 |
| 3.4 | **Compressed report ingestion (gzip)** (new card; Ed, 2026-10-03) | S | — |

### Phase 4 — Ship it

| # | Card | Size | Depends on |
|---|---|---|---|
| 4.1 | **Release Pipeline & Published Images** (new card) | M | 0.3 |
| 4.2 | **Self-hosted Deployment Guide (TLS, backups, upgrades)** (new card) | M | 4.1, 2.2 |
| 4.3 | Dogfooding: Provenly CI publishes to Provenly | M | 3.3, 4.2 |
| 4.4 | Public Readiness Gate (readiness checks; the repo is already public) | M | everything above |

Critical path: **0.3 → 2.1 → 2.2 → 3.2 → 3.3 → 4.3 → 4.4**. Phases 1 and 4.1 run alongside it.

Out of the MVP (2026-10-02): **Run Scope for Partial Runs** (D2 → Test Suite design, Notion Incubator) and **manual
execution** (D3 → Incubator, *Pending design — Manual testing*).

## 4. Decisions (taken by Ed, 2026-10-02)

All five are in the Notion Decision Register ("MVP D1"…"MVP D5").

| ID | Question | Decision |
|---|---|---|
| D1 | **Retries and the logical result** (Notion #7): a test that fails then passes on retry? | **Accepted.** Results carry an *attempt*; the logical result per execution is the last attempt; a pass after failures is `passed` + **flaky**, whatever the cause (environmental or not — cause analysis is Quality Intelligence, later). Browser/platform variants keep failed > error > skipped > passed. |
| D2 | **Partial runs**: how does a smoke run of 10 of 100 automated TCs avoid 90 `untested`? | **Deferred, not POC/MVP.** It belongs to the Test Suite Model & Automation Synchronization design (Incubator): a run's expected universe will come from the resolved suite selection captured on the TestRun. Until then it is the active automated catalog. |
| D3 | **Manual testing in the MVP** (Notion #4) | **Deferred to after the MVP.** The MVP keeps the manual catalog (test cases with steps) but no manual execution; an in-depth analysis is tracked in Incubator (*Pending design — Manual testing*). |
| D4 | **Secrets at rest** | **Accepted.** Envelope encryption with a KEK from the environment (`PROVENLY_SECRET_KEY`), rotation by re-encrypting; external KMS/Vault later behind the same port. |
| D5 | **Repository name and visibility** | **Accepted.** Stays `edcrove/Provenly` (personal project). It is already public; the Public Readiness Gate keeps the readiness checks, including secrets in history. |
| D6 | **Compressed JUnit reports** (2026-10-03, validation of CI/CD Result Ingestion API) | **MVP.** Accept `Content-Encoding: gzip` on ingestion with the size limit applied to the decompressed body; the POC answers any encoding other than identity with a clear 415. Card 3.4. |
| D7 | **Concurrent edits of a test case** (2026-10-04, after the POC validation) | **MVP, optimistic locking.** Test case and step reads return an `ETag`; `PATCH`/`PUT`/`DELETE` accept `If-Match` and answer **412** when it no longer matches (someone saved in between). `If-Match` is optional in the API (clients without it keep last-write-wins); the UI always sends it and shows a conflict notice with a reload. Today the API is last-write-wins: same-field edits are silently lost. Card 1.5. |

## 5. Risks

| Risk | Mitigation |
|---|---|
| Live streaming is the largest change (new lifecycle, events, WebSocket, River) | Execution Session protocol first (REST), WebSocket second; final report stays authoritative so a broken stream never loses data. |
| Auth retrofitted onto every route and every test layer | One middleware + test helpers; contract tests run authenticated and add 401 variants automatically. |
| Toolchain majors (TS 7 / `openapi-typescript`) may block | Card allows documenting the blocker and moving on. |
| Notion free-plan API limits slow down documentation | Batch Notion updates; repo docs remain usable on their own. |

## 6. Board organisation

- Trello *Por hacer* is ordered by this plan (phase, then order); each card starts with `Fase N.x`.
- New cards: Retries & Logical Result, Playwright Reporter, Release Pipeline & Published Images, Self-hosted Deployment
  Guide. Run Scope for Partial Runs stays on the board marked out of the MVP, as input for the Test Suite design.
- Decisions D1–D5 are in the Notion Decision Register (2026-10-02).
