# Maturity assessment and 1.0 plan (proposal, 2026-10-12)

Status: **proposal for Ed; nothing here is decided.** Asked by Ed on 2026-10-12: review the prototype, say how mature
it is, and plan what separates it from the MVP, from a complete 1.0, and what material exists for an advanced system.
Facts were checked live (repo at `prototype/full-product` e85c36d, GitHub, Trello, Notion); the technical and product
reviews come from the `architect` and `product-owner` agents (review panel) plus my own checks. Sources: `docs/mvp-plan.md`
(the MVP as planned), `docs/prototype/README.md` (what was built and decided), `docs/implementation-decisions.md`,
Notion Incubator and Decision Register.

## 1. Where the product stands

| Measure | Value |
|---|---|
| Features | 21 prototype features + 2 audit rounds (PRs #25–#83), all merged into `prototype/full-product` |
| Contract | 91 operations in `api/openapi.yaml`, 39 migrations, 8 Go modules (`catalog`, `execution`, `ingestion`, `identity`, `integrations`, `insights`, `audit`, `mcp`) |
| Code | backend ≈16.5k lines (+20k of tests), frontend ≈12.8k (+12.4k of tests and journeys), reporter 0.1.0 |
| Tests | 10 gates at 100%; inventories: 78 BE-INT, 69 FE-INT, 33 BE-E2E, 39 FE-E2E; contract 610 + 541 variants; probe 936 checks; E2E green on Render (55/55) |
| Process | one PR per card, review panel (6 experts, architect, PO, 6 personas), decisions in Notion + repo |

What is **not** in place, verified today:

- `main` is still the POC (5245a4c, 2026-10-04). The prototype is 299 commits / 551 files ahead; none of it has gone
  through a PR to `main`. The MVP exit criterion "CI green and required on `main`" holds for the POC only.
- **No tag, no release.** `release.yml` has never run: there are no GHCR images, no npm package, no GitHub release.
  Self-hosting from "published images" (MVP exit criterion 1) is not yet possible; Render builds from source.
- **Dogfooding is wired but unverified**: the E2E job reports through the reporter only when `PROVENLY_URL` /
  `PROVENLY_API_KEY` secrets exist; nothing in the repo or the Render instance proves they do.
- **Trello is behind reality**: *Por hacer* still lists the 15 MVP cards of phases 1–4 (auth, login UI, retries, live
  runs, reporter, gzip, release, self-hosting, dogfooding, public readiness…) that the prototype implemented; 22 cards
  wait in *Validation* for Ed; *En progreso* is empty.
- **Decisions taken without Ed**: the prototype's decision log has ~100 `Pn-m` entries (`docs/prototype/README.md`),
  reviewed by the review panel but not by Ed. They are the product's actual behavior today.

## 2. Maturity verdict

**The prototype is past the MVP as planned and is a feature-complete 1.0 candidate that has never been released.**
Functionally it exceeds `docs/mvp-plan.md` (it also has what the plan left out: suites, manual execution,
requirements, issues, dashboard, integrations, MCP, audit). What it lacks is not features but the *release, adoption and
operation* layer: a shipped version, a `main` that reflects it, a backlog that matches it, Ed's sign-off on the
decisions, and a few operational gaps a team running it for a year would hit (below).

Grade per area (prototype / MVP-ready / 1.0-ready / advanced):

| Area | Grade | Why |
|---|---|---|
| Domain and data model | 1.0-ready | TC identity, snapshot universe, per-project keys, append-only results and triggers enforcing invariants (migrations 00009–00012, 00038); versions of test cases and per-project run numbers are the known gaps (Incubator) |
| API | 1.0-ready | Contract first, problem+json, validated both sides, 91 ops; `info.version` is 0.1.0 and there is no deprecation policy yet |
| Backend architecture | 1.0-ready | Modular monolith with ports (`authz.Guard`, `ResultReader`), sqlc per module; catalog is the largest module (A6 card: split traceability) |
| Frontend | MVP-ready → 1.0 | Every flow exists and is tested; project lives in browser storage (no project URLs), English only, no bulk actions |
| Testing | advanced | 10 gates at 100%, contract on both sides, probe, fuzz, E2E local and deployed, inventories tied to cards |
| Security | 1.0-ready | Sessions, roles, API keys, PATs, secrets at rest, audit, SSRF guard, headers, sign-in throttle; no SSO, throttle is per instance (`docs/self-hosting.md` §6), no global rate limit (Incubator) |
| Operability / deploy | MVP-ready | Compose, envs, backups profile, self-hosting guide, Render blueprint; **never released**, no upgrade rehearsal from a tagged version, single instance by design |
| Observability | MVP-ready | OTel traces and structured logs; no metrics endpoint, no alerting guidance |
| Performance / scale | MVP-ready | Pagination everywhere, O(1) outcome reads (card #44), 100k-result history checked; no load test, no retention for results/audit by decision |
| Dev process | advanced | PR per card, hooks, skills, review agents, decisions register; Trello drift is the weak point |

## 3. MVP: exit criteria (`docs/mvp-plan.md` §2) against today

| Criterion | State | Evidence |
|---|---|---|
| Deploy from published images with the guide, TLS, backups | **Partial** | guide and compose exist (`docs/self-hosting.md`, backup profile #63); no published image (no tag) |
| Playwright suite reports live with retries and shards; JUnit for others | **Met** | features 7, 15, 16, card #57; BE-E2E/FE-E2E live and sharded journeys |
| Every route authenticated; ingestion by API key | **Met** | features 2–4; contract 401/403 variants on every op |
| Provenly's CI publishes its runs to a Provenly instance | **Unverified** | reporter wired in `ci.yml`; secrets unknown |
| Traces and correlated logs | **Met** | feature 17 |
| 10 gates at 100%, CI green and required on `main` | **Partial** | gates PASS on the prototype branch; `main` is the POC |
| Public Readiness Gate | **Partial** | SECURITY.md, pinned actions, vuln scans exist; the gate was never run as a release checklist |

Verdict: **MVP functionally complete, formally not closed.** Closing it is a release exercise, not a build exercise.

## 4. 1.0: what a team still misses

A 1.0 is a version a real team (QA lead, SDET, developer, manual tester, DevOps, engineering manager) adopts as its
system of record and runs for a year. Two kinds of gaps, from the product-owner and architect reviews, both verified.

### 4.1 Product (by persona)

**Must-have for 1.0**

| Gap | Who | Source | Size |
|---|---|---|---|
| Published, versioned images and an upgrade rehearsed once from the previous tag | DevOps | MVP exit 1; Notion 07 | S/M |
| A durable instance (not Render's 30-day free DB) and one pilot team's feedback loop | EM | MVP D8 / DEC-66 | M (infra) + Ed |
| `ciUrl`, commit and PR links on runs (the developer's first click from a red run) | Developer | prototype README §26, still open | S |
| Failure triage basics: failures grouped by message across runs, "first failed in run N", run comparison | SDET, QA lead | Notion 21, 27 | M + M |
| A live run cannot stay "running" forever (abandoned-run timeout / finalize) | SDET | P15-6 | S |
| Export of runs, results and history (CSV/JSON) | EM | Notion 02 "open data by design"; Incubator ExportSink | S/M |
| Project-scoped URLs so a shared link carries its project | QA lead | Incubator (deployed audit) | M |
| A second native reporter or documented recipes (pytest, Jest/Vitest, JUnit 5) beyond Playwright | Developer, SDET | Notion 04 | M each |
| API stability statement, CHANGELOG, version stamped from the tag (`info.version` is 0.1.0 "POC") | EM, DevOps | Notion 07 | S |
| Toolchain majors (TS 7, MSW 3, Node 24) | maintainers | DEC-53, card in *Por hacer* | M |

**Should-have (1.x)**: step-level manual results with evidence attachments (object storage) · test plans / campaigns
with assignment and progress · Jira connector (issues and requirements pull, status mapping) · notifications
(Slack/email on the webhook outbox) · metrics and alerting · per-project run numbers · test case versions · CSV import
of test cases (migration from TestRail/Zephyr) · global sign-in rate limit · audit retention and export.

**Nice-to-have**: SSO/OIDC, environments and releases on runs, configurable dashboards, MCP write tools behind
grants, Helm chart, signed images and SBOM, run-level artifacts.

### 4.2 Technical (architect, verified)

| # | Item | Why it matters for a year of real data | Size |
|---|---|---|---|
| 1 | **Persisted run outcomes** (`test_run_outcomes` written when a run completes; lists, history and dashboard read it) | Today every list page recomputes each run's outcome from all its result and snapshot rows (`execution/service.go` `attachOutcomes` → `ListSummaryInputs`): a 3,000-test project moves ~120k rows per runs-list page. The one item that fails on data growth alone; also unblocks a verdict filter | M |
| 2 | **SQL paging and grouped counts** for issues, requirements, suites, dimensions | These four lists are read whole and sliced in the handler (`pagination.Slice` in `*_http.go`), with verification/coverage derived per item; a GitHub sync creates thousands of issues in one click | M |
| 3 | **Pool size, `statement_timeout`/`lock_timeout`, handler deadline** | None exist (`platform/postgres`, `platform/server`): a slow query holds a connection until the client leaves; `FOR UPDATE` paths can queue forever. Slow must become an error, not an outage | S |
| 4 | **Transactional outbox for `run.completed`** | Webhook enqueue happens after the run commits: a crash in between drops the event silently (documented) | M |
| 5 | **Sign-in lockout in the database, or "single replica" stated in the guide** | The throttle is a process map (`identity/throttle.go`); a second replica weakens it | S |
| 6 | **Metrics** through the OTel SDK already installed: HTTP latency by route, pool stats, pending/failed deliveries, ingestion errors | Traces without metrics give nothing to alert on | M |
| 7 | **Live endpoint `?since=<sequence>`** or SQL-side state | Each viewer polls the whole run (up to 10k events) every 2 s: the only cost that multiplies by viewers | M |
| 8 | **Storage policy for `test_run_events` and old attempts** (decision + code) | Results are never purged by decision; live events have no reason to live forever | S + S |
| 9 | **Frontend route splitting and per-feature query modules** | One 580 kB eager bundle (no `lazy()` in `routes.tsx`); every hook in a 1,193-line `queries.ts` with two key conventions | S + M |
| 10 | **Release metadata and policy**: version from the tag, remove "POC" from the spec, a 5-line compatibility policy, CI upgrade test from the previous tag's seed | Self-hosting is not real until `IMAGE_TAG` resolves and an upgrade has been rehearsed | S/M |

Also: `provenly rotate-secrets-key` (the `v1:` prefix allows it, S); `timeout-minutes` on CI jobs (S); an index for the
run filters when a relative-cost check shows it (S); make the traceability seam explicit inside `catalog` (A6 card) so
a later `internal/traceability` module is a directory move (S now).

## 5. Advanced: material available

**With material (design notes, decisions, partial code)** — these can be refined into cards without starting from zero:

- *Quality intelligence*: Notion 21 candidate list; `insights` module with automation, stale and flaky counters,
  trend, coverage and verification (feature 14). Missing: durations, releases/environments, failure clustering.
- *Requirements and issue federation*: Notion 06 authority model and sync states; DEC-8 verification matrix
  implemented; push import plus GitHub mirror. Missing: pull sync, conflict states, write-back.
- *Connectors and SDK*: GitHub connector, secrets box, outbox, SSRF guard are real foundations; no connector port or
  registry yet (`integrations` hard-codes GitHub and one event). Designing the port comes with the second provider.
- *ExportSink*: the webhook outbox is the first sink; Notion 15 "sinks decoupled". No second sink.
- *MCP and agents*: DEC-10, 13 read tools over PATs, re-entry criteria in Notion 06. Write tools need an authz decision.
- *Test suites*: Incubator design (static/dynamic/templates/discovery) plus the P10 implementation. Discovery, dry-run
  and automation sync only described.
- *Manual workflows*: Incubator analysis list plus P11. Step results, evidence and campaigns only described.
- *Shard re-run inheritance, global sign-in limit, project-scoped URLs, per-project run numbers, test case versions*:
  problem, scope and parity prompts written in the Incubator; no code.

**Only a title or a line** — need the Incubator refinement step before anything else: multi-tenancy and tenant
isolation, OAuth/SSO and advanced RBAC, AI-assisted journeys, Azure DevOps connector, plugin SDK, releases/environments,
artifacts/object storage, retention policies and analytics store, Prometheus export, Kubernetes/Helm, configurable
dashboards.

**Structural debts an advanced version would hit** (architect): the project is the tenancy unit and `TC` is hard-wired
as the ingestion fallback (isolate that behind one function now, S; an organization layer can wrap projects later
without touching module boundaries); HA needs items 4–5 of §4.2 only if HA is a 1.0 claim; snapshot rows per run and
all-attempt results grow with runs × catalog (counters before 1.0; partitioning can wait past ~10^8 rows); the 6k-line
`openapi.yaml` is additive-only with no deprecation mechanism (policy before 1.0, split later); the 2,381-line
hand-written MSW mock is the testing cost that grows with contributors (mock-parity tests, later).

## 6. Decisions for Ed before 1.0

1. **DEC-88** (Proposed in Notion): D8–D13 are Accepted and implemented; remove "paused" from
   `docs/prototype/README.md` line 16 and `.claude/agents/product-owner.md` line 37. Recommendation: accept.
2. **Landing the prototype on `main`**: validate the 22 *Validation* cards in batches, then one merge PR (the branch
   forks from `main`'s head, the merge is clean), then tag `v0.1.0`. Recommendation: yes, before any new feature.
3. **Ratify the beyond-MVP features as the 1.0 baseline** with their `Pn-m` decisions: taxonomy, suites, manual runs,
   requirements, issues, dashboard, webhooks, GitHub, MCP, audit. Suites (P10) and manual execution (P11) were built
   although D2/D3 sent them to in-depth design with parity input; the Incubator pages still read "not committed scope".
   Recommendation: ratify as baseline, keep the deep-analysis items as 1.x, update the Incubator.
4. **Product-shaping decisions taken without you** that deserve an explicit yes: P1-2/P1-3 (default project `TC`,
   numeric ids in URLs), P2-2/P2-3 (stateless 12 h sessions, no per-session revocation), P3-2 (admin-only project
   creation), P4-2 (keys ingest-only), P7-1 (retries only with a signal), P9-2 (seven built-in dimensions), P10-2/P10-7
   (suite universe at ingest; one suite per run), P11-2/P11-3 (manual default scope; re-test is not flaky), P12-2
   (push-only import), P13-4 (fixed worst-link aggregation vs Notion 05 "configurable"), P14-3/P14-4 (14-day / 20-run
   defaults), P15-3/P15-6 (polling; no abandoned-run timeout), P18-2 (one webhook event), P20-1 (reads not audited),
   P21-4 (in-memory throttle). Recommendation: accept all but P15-6 (fix in 1.0, §4.1) and record P13-4 as a deviation.
5. **Release scope**: multi-arch amd64/arm64 yes (S); SBOM and signing later; reporter to npm with the first tag.
   The Trello card asks for multi-arch and SBOM; `release.yml` has neither.
6. **Durable dogfood / pilot instance**: a small paid host with the compose `backup` profile, CI secrets set so the
   suite reports itself. Recommendation: now, it is the cheapest way to meet D8.
7. **`ciUrl`** (open since the deployed audit). Recommendation: yes, S.
8. **Storage policy** for live events and old attempts (§4.2 item 8), and whether "single replica" is a 1.0 statement
   or HA is a 1.0 claim (items 4–5).
9. **Trello**: reconcile the 15 built *Por hacer* cards (link each to its prototype PR and evidence, approve or
   archive), then replace `docs/mvp-plan.md` with this plan once decided. GitHub Issues migration (Notion 08 runbook)
   after the board is reconciled.

## 7. Proposed plan

Sizes as in `docs/mvp-plan.md` (S ≈ 1 day, M ≈ 2–4 days, L ≈ a week or more of implementation, before validation).
Recommendation: **A and B before any new feature** — the product's risk today is unmerged, unapproved, unreleased work,
not missing capability. C turns "feature-complete" into "system of record"; D and E only after a pilot team says
what hurts.

### Phase A — Ratify and land (goal: the prototype is `main`; Notion, Trello and the repo agree)

| # | Item | Size | Depends on |
|---|---|---|---|
| A.1 | Decisions 1, 3, 4 recorded (Notion Decision Register + repo), "paused" removed, Incubator updated | S | Ed |
| A.2 | Ed validates the 22 *Validation* cards in 3 batches (`validate-card`; evidence comments exist) | Ed's time | — |
| A.3 | Reconcile the 15 built *Por hacer* cards: link to PR and tests, approve, or archive | S | A.2 |
| A.4 | Merge PR `prototype/full-product` → `main`; `main` protected with CI required | S | A.2 |
| A.5 | Docs drift: README/CLAUDE.md list 8 modules; reporter name; `integrations` depends on `execution`; paging and outcome notes in `architecture.md`; remove "POC" from the spec and testing strategy; replace `docs/mvp-plan.md` with this plan | S | A.1 |

### Phase B — Ship 0.1 (goal: the 7 MVP exit criteria met)

| # | Item | Size | Depends on |
|---|---|---|---|
| B.1 | Tag `v0.1.0`: GHCR images, npm reporter, GitHub release; version stamped from the tag everywhere | S | A.4 |
| B.2 | Multi-arch images (amd64/arm64); SBOM/signing deferred | S/M | B.1 |
| B.3 | Durable dogfood instance with backups; CI secrets set; history visible | M (infra) | B.1, decision 6 |
| B.4 | Public readiness leftovers: CODE_OF_CONDUCT, CHANGELOG, `docs/README.md` index, public roadmap, history secret-scan evidence; run the gate as a checklist | S | — |
| B.5 | Timeouts, pool settings, `timeout-minutes` on CI jobs (§4.2 item 3) | S | — |
| B.6 | CI upgrade test: migrate the previous tag's seed, then run the smoke flow | S/M | B.1 |
| B.7 | Toolchain majors (TS 7, MSW 3, Node 24) | M | A.4 |

### Phase C — 1.0 (goal: a pilot team lives on it for a year)

| # | Item | Size | Depends on |
|---|---|---|---|
| C.1 | `ciUrl`, commit and PR links on runs | S | decision 7 |
| C.2 | Abandoned live run timeout / finalize | S | decision 4 (P15-6) |
| C.3 | Persisted run outcomes (+ verdict filter) | M | — |
| C.4 | SQL paging and grouped counts for issues, requirements, suites, dimensions | M | — |
| C.5 | Failure grouping across runs, "first failed in run N" | M | C.3 |
| C.6 | Run comparison (two runs; branch vs main) | M | C.5 |
| C.7 | Export CSV/JSON of runs, results, history | S/M | — |
| C.8 | Project-scoped URLs (Incubator design first) | M | — |
| C.9 | Second reporter or recipes (pytest, Jest/Vitest, JUnit 5 live) | M each | B.1 |
| C.10 | Transactional outbox for `run.completed`; lockout store or "single replica" statement | M + S | decision 8 |
| C.11 | Metrics (HTTP, pool, deliveries, ingestion) and alerting guidance in the self-hosting guide | M | — |
| C.12 | Live `?since=` | M | — |
| C.13 | Frontend route splitting, per-feature query modules, shadcn a11y regeneration (decision 11) | S + M | — |
| C.14 | API compatibility policy, CHANGELOG discipline, storage policy for live events | S | decision 8 |
| C.15 | Pilot feedback loop: a review-panel persona pass on the pilot's real data, fixes | Ed + M | B.3 |

Order inside C: C.1–C.4 first (cheap and the scale guards), then C.5–C.7 (triage), C.8–C.9 (adoption), C.10–C.14
(operations), C.15 throughout. 1.0 is tagged when the pilot has run a release cycle on it.

### Phase D — 1.x should-haves

Notifications on the outbox (M, after C.1) · manual step results and evidence attachments (L, needs object storage
decision) · test plans / assignment / progress (L) · Jira connector with a connector port (L) · per-project run numbers
(M) · CSV import of test cases (M) · global sign-in rate limit (S) · audit retention and export (S) · traceability
split out of `catalog` (M, with the Jira connector) · `rotate-secrets-key` (S).

### Phase E — Advanced bets

Test case versions (L) · MCP write tools behind explicit grants (M) · SSO/OIDC (M/L) · connector SDK and event
catalogue (L) · quality intelligence v2: durations, releases/environments, clustering (L) · multi-tenancy only on
demonstrated demand (XL) · AI-assisted journeys after the surfaces above stabilise (L).
