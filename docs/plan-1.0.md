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

(To be consolidated with the product-owner report: must / should / nice, by persona.)

## 5. Advanced: material available

(To be consolidated: Incubator items with design notes vs titles only.)

## 6. Decisions for Ed before 1.0

(Drift and `Pn-m` decisions that deserve explicit review.)

## 7. Proposed plan

(Phases, cards, sizes, order.)
