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
| 1 | Projects and per-project TC keys (`CHK-12`) | MVP D9, D10 | ⏳ | |
| 2 | Users, login (JWT) and invitations | MVP D13, DEC-30 | ⏳ | |
| 3 | Roles and project membership (Admin, Maintainer, Member, Viewer) | MVP D12 | ⏳ | |
| 4 | API keys for CI, `?project=` ingestion, secrets at rest | MVP D4, D11 | ⏳ | |
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
| 18 | Export sink (webhooks) and GitHub connector | Planning #3, #21, Incubator | ⏳ | |
| 19 | MCP server (agent interface) | Incubator, DEC-10 | ⏳ | |
| 20 | Audit log | Incubator (Project & Authorization) | ⏳ | |
| 21 | Release pipeline, self-hosting guide, dogfooding, public readiness | Trello phase 4 | ⏳ | |

## Decision log

Decisions taken in the prototype without Ed (to review). `MVP Dn` and `DEC-n` are Ed's decisions, applied as written.

| Id | Topic | Decision | Why |
|---|---|---|---|

## What each feature does

Filled in as each feature is merged: behavior, API, UI, tests, known limits.
