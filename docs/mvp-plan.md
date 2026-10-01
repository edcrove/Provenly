# MVP plan (proposal, 2026-10-01)

Status: **proposal for Ed's review**. Notion stays canonical for *why* (decisions), Trello for execution (cards,
order, validation), this file for the plan as a whole. Nothing here is decided until Ed accepts it; open decisions are
listed in §4 and block only the cards that depend on them.

## 1. Goal

The POC proved the core loop locally: a stable `TC-<id>`, CI ingestion of JUnit, an immutable expected-universe
snapshot, an honest summary and history, with every test layer at 100%.

**The MVP makes Provenly usable by one real team, every day, on a server they own:**

1. **Secure to expose** — CI ingests with an API key, people log in, nothing secret leaks.
2. **Live and trustworthy runs** — a native Playwright reporter streams the run while it executes; the final report
   stays authoritative and is reconciled against the live stream.
3. **Correct results for real suites** — retries/flaky tests, shards and partial (smoke) runs no longer distort the
   summary.
4. **Operable** — published images, a documented self-hosted deployment (TLS, backups, upgrades), OpenTelemetry.
5. **Proven on itself** — Provenly's own CI reports to a Provenly instance (dogfooding).
6. **Public** — the repository passes the Public Readiness Gate under Apache-2.0.

Out of scope (Post-MVP, unchanged): multi-tenancy, OAuth/SSO, connectors (GitHub/Jira, Notion #3), Issue
Verification (#8), ExportSink (#21), taxonomy and Smart Suites (#26/#27), MCP (#22), AI journeys (#23).

## 2. Exit criteria (MVP Definition of Done)

- A team deploys Provenly from published images with the self-hosting guide, behind TLS, with automated backups.
- Their Playwright suite reports live (with retries and shards) and via JUnit for any other framework.
- Every API route except `/healthz` and `/readyz` requires authentication; ingestion uses an API key.
- Provenly's own CI publishes its runs to a Provenly instance; the history is visible.
- Traces and correlated structured logs exist for API requests and ingestion.
- The 8 gates + 2 consolidated gates stay at 100%; CI is green and required on `main` (ruleset).
- The repository is public.

Every MVP card also follows the standard card DoD: OpenAPI first, migrations, tests in every layer it touches on both
sides, gates at 100%, docs and screenshots updated, manual validation by Claude, Hecho moved by Ed.

## 3. Phases and order

Sizes: S ≈ 1 day, M ≈ 2–4 days, L ≈ a week or more (Claude implementation time, before validation).

### Phase 0 — Close the POC (now)

| # | Item | Owner | Depends on |
|---|---|---|---|
| 0.1 | Validate the remaining POC cards in *Validation* (13 left, one by one) | Ed + Claude | — |
| 0.2 | `main` ruleset (PR required, required checks, no force push) — CI Pipeline card | Ed | — |
| 0.3 | Merge PR #1, set `main` as default branch | Ed | 0.1, 0.2 |
| 0.4 | Close POC in Notion (Control Center status, consolidated page) | Claude | 0.3 |

### Phase 1 — Foundations (parallel, low risk)

| # | Card | Size | Depends on |
|---|---|---|---|
| 1.1 | Toolchain Major Upgrades (TS 7, MSW 3, Node 24) | M | 0.3 |
| 1.2 | OpenTelemetry Basic Instrumentation | M | 0.3 |
| 1.3 | Snapshot Amendment per Run (DEC-42) | M | 0.3 |
| 1.4 | **Decisions D1–D3 (§4)** written in the Decision Register | Ed | — |

1.3 is the last POC follow-up (already decided); doing it early keeps the summary model stable before live runs
change ingestion.

### Phase 2 — Secure by default

| # | Card | Size | Depends on |
|---|---|---|---|
| 2.1 | Secrets Abstraction | M | D4 |
| 2.2 | Authentication (API Key + JWT) | L | 2.1 |
| 2.3 | Login UI | M | 2.2 |

### Phase 3 — Live, correct runs (the MVP's product value)

| # | Card | Size | Depends on |
|---|---|---|---|
| 3.1 | **Retries & Logical Result** (new card) | M | D1 |
| 3.2 | Live Test Run Streaming & Reconciliation (incl. Execution Session protocol #9, shards) | L | 2.2, 3.1 |
| 3.3 | **Playwright Reporter (first native reporter)** (new card) | L | 3.2 |
| 3.4 | **Run Scope for Partial Runs** (new card) | M | D2 |

### Phase 4 — Ship it

| # | Card | Size | Depends on |
|---|---|---|---|
| 4.1 | **Release Pipeline & Published Images** (new card) | M | 0.3 |
| 4.2 | **Self-hosted Deployment Guide (TLS, backups, upgrades)** (new card) | M | 4.1, 2.2 |
| 4.3 | Dogfooding: Provenly CI publishes to Provenly | M | 3.3, 4.2 |
| 4.4 | Public Readiness Gate → repository public | M | everything above, D5 |

Critical path: **0.3 → 2.1 → 2.2 → 3.2 → 3.3 → 4.3 → 4.4**. Phases 1 and 4.1 run alongside it.

## 4. Decisions needed from Ed

| ID | Question | Why it matters | Recommendation |
|---|---|---|---|
| D1 | **Retries and the logical result** (Notion #7): a test that fails then passes on retry — passed, failed or *flaky*? | Today two results of one TC-ID aggregate as `failed` (correct for browsers, wrong for retries). Playwright's generated config retries failed tests on CI. | Results carry an *attempt*; the logical result per execution is the last attempt; a pass after failures is `passed` + a **flaky** flag shown in summary and history. Browser/platform variants keep failed > error > skipped > passed. |
| D2 | **Partial runs** (POC assumption): how does a smoke run of 10 of 100 automated TCs avoid 90 `untested`? | Real pipelines run subsets; the summary would mislead. | CI declares the scope at run start (`scope=full` default, or a list/tag of TC-IDs) and the snapshot is that scope. Smart Suites stay Post-MVP. |
| D3 | **Manual testing in the MVP** (Notion #4) | Defines whether the MVP is automation-only. | Keep MVP automation-first: manual *catalog* (done) but no manual execution UI; revisit after dogfooding. |
| D4 | **Secrets at rest**: environment-provided key (KEK) + encrypted column, or an external store (Vault/KMS)? | Needed before API keys and future provider tokens are stored. | Envelope encryption with a KEK from the environment (`PROVENLY_SECRET_KEY`), rotation by re-encrypting; external KMS later behind the same port. |
| D5 | **Going public**: when, and under which org/name? | Last gate of the MVP. | After 4.3, under `edcrove/provenly` (rename case) or a `provenly` org. |

## 5. Risks

| Risk | Mitigation |
|---|---|
| Live streaming is the largest change (new lifecycle, events, WebSocket, River) | Execution Session protocol first (REST), WebSocket second; final report stays authoritative so a broken stream never loses data. |
| Auth retrofitted onto every route and every test layer | One middleware + test helpers; contract tests run authenticated and add 401 variants automatically. |
| Toolchain majors (TS 7 / `openapi-typescript`) may block | Card allows documenting the blocker and moving on. |
| Notion free-plan API limits slow down documentation | Batch Notion updates; repo docs remain usable on their own. |

## 6. Board organisation

- Trello *Por hacer* is ordered by this plan (phase, then order); each card starts with `Fase N.x`.
- New cards: Retries & Logical Result, Playwright Reporter, Run Scope for Partial Runs, Release Pipeline & Published
  Images, Self-hosted Deployment Guide.
- Decisions D1–D5 go to the Notion Decision Register as *Proposed* when Ed starts the planning review.
