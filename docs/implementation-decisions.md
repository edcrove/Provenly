# Implementation decisions pending review

Decisions taken while implementing the POC that the Decision Register / Domain Model did not settle. Each one is
small and reversible; they are flagged here for Ed's review (Notion stays canonical for *why*).

1. **Ingestion transport.** `POST /api/v1/ingestion/junit` receives the raw JUnit XML body (`application/xml`) and
   the run metadata as query parameters (`provider`, `runId`, `runAttempt`, `pipeline`, `branch`, `commit`). Chosen so
   CI can use a plain `curl --data-binary @report.xml`. `pipeline`, `branch`, `commit` are optional.
   **Status: Accepted by Ed (2026-09-28).** Future option (not in POC): `multipart/form-data` with a `report` file
   part plus one field per metadata item, if ingestion needs several files per request or many more fields.
2. **Valid results outside the snapshot.** A result with a valid TC-ID whose test case was *not* in the run snapshot
   (active but `automated=false`) is persisted as `valid`, shown in the run detail, but excluded from the universe and
   percentages and counted separately as `outsideUniverse` in the summary. The Domain Model only specifies the
   exclusion of *invalid* TC-IDs.
   **Status: Accepted by Ed (2026-09-28).** Follow-up requested: when a run has `outsideUniverse` results, the UI
   warns that the TC receives automated results while marked manual, and the user can (1) mark the TC
   `automated=true` and (2) explicitly include it in *that* run's universe so the summary reflects it.
   (1) is **implemented**: the summary exposes `outsideUniverseTestCaseIds`; the run detail lists them with a
   "Mark as automated" action and the test case page warns when a manual TC has results (FE-INT-010/014,
   FE-E2E-006). Marking affects future runs only.
   (2) conflicts with the frozen invariant "the snapshot is never modified after creation", so it needs a Decision
   Register change first. Proposed shape: an explicit, audited *snapshot amendment* per run (who, when, which
   TC-ID, only TC-IDs that already have valid results in the run), never an automatic recomputation.
3. **Deprecated TC-IDs in results.** Following "testCaseId when the TC-ID is valid", results that reference a
   deprecated test case keep only `requestedTestCaseId` + `correlation=deprecated`; they are therefore not listed in
   that test case's history.
   **Status: Changed by Ed (2026-09-28) — option B, implemented.** Deprecated results now keep `testCaseId`
   (migration 00003: the TC-ID link is required for `valid` and `deprecated` correlations), appear in the test
   case's history marked "after deprecation", and remain excluded from summaries as diagnostics. Domain Model
   wording "testCaseId when the TC-ID is valid" should be updated to "valid or deprecated".
4. **Malformed rules.** `tc-id` accepts `153` or `TC-153`; `0`, leading zeros, non-digits, >18 digits, an empty
   value, or several *different* ids (property or name) are `malformed`.
   **Status: Decided by Ed (2026-09-28), implemented.** (a) Leading zeros are accepted and normalized
   (`TC-0153` == `TC-153`; `0`/`TC-000` stay malformed). (b) Only uppercase `TC-` counts in names, for
   consistency and as a code convention (`tc-153` is `missing`). (c) Several different ids in one `<testcase>` stay
   `malformed`; data providers / parameterized tests are supported because each invocation is its own
   `<testcase>` and declares its own id (covered by a parser test). Limitation: a `tc-id` property placed at
   `<testsuite>` level, or one name listing all ids, is not supported.
5. **Parse errors.** A testcase without `name` or with an invalid `time` is reported in `parseErrors` and not
   persisted; the rest of the batch continues.
   **Status: Changed by Ed (2026-09-28) — options B + C, implemented.** Only nameless testcases are discarded; an
   invalid `time` keeps the result with an unknown duration. `durationMs` (JUnit `time` is in seconds, stored as
   rounded milliseconds) is nullable: null = not reported/invalid, 0 = reported as 0 or under 0.5 ms (UI: "—" vs
   "<1 ms"); skipped tests keep whatever duration the framework reports. Parse errors are stored with the run
   (`test_run_parse_errors`, migration 00004) with `persisted` kept/discarded, exposed by
   `GET /api/v1/test-runs/{id}/parse-errors`, shown in the run detail, and returned on idempotent replays.
   Parse errors have a `severity` (migration 00005): `error` (no name → discarded; invalid time → kept without
   duration) or `warning` — for now a **passed or failed** result with a 0 ms duration is kept and flagged for
   review (skipped/error results with 0 ms are not flagged).
6. **Replay response.** An idempotent replay returns `200` with the existing run and the diagnostics stored at
   creation; nothing is re-parsed into the database.
   **Status: Changed by Ed (2026-09-28) — option B, implemented.** The run stores the SHA-256 of the report that
   created it (migration 00006, internal field). A replay whose report differs still changes nothing, but the
   response carries `warnings: ["report differs from the one already ingested for this attempt; it was not applied
   (send a new runAttempt to record it)"]`; an identical replay has `warnings: []`.
7. **Run lifecycle in the POC.** Runs are created synchronously when the final report is ingested and stored as
   `completed`; `startedAt` is the earliest `testsuite@timestamp` when present.
   **Status: Changed by Ed (2026-09-28) — option B, implemented.** CI may send `status=completed|failed|cancelled`
   (default `completed`) with the final report, for pipelines that broke or were stopped. The run keeps that status
   (a replay never changes it and warns if it differs); the UI badges it and flags failed/cancelled runs as possibly
   incomplete ("untested may simply not have run"). Test outcomes stay in the summary. Live streaming (Core MVP) will
   improve this with created/running and reconciliation.
8. **Steps.** At most 100 steps per test case (so one page of the paginated steps listing always holds them all);
   reorder is `PUT /steps/order` with the full permutation of step ids.
   **Status: Accepted by Ed (2026-09-28).**
9. **Summary rounding.** Percentages are 0..100 rounded to 2 decimals; 0 when the denominator is 0.
   **Status: Changed by Ed (2026-09-28), implemented.** The API sends 6 decimals; the UI rounds to 2 only for
   display and computes totals from the precise values (3 x 33.333333 shows a "Total 100%" row). A 0 denominator
   stays 0% (no null) and the UI always shows the counts next to it ("0 of 0 test cases executed").
10. **Deprecation is one-way** in the POC (no re-activation endpoint); editing a deprecated test case is allowed.
    **Status: Changed by Ed (2026-09-28) — option B, implemented.** `POST /api/v1/test-cases/{id}/reactivate`
    (idempotent) brings a deprecated test case back to `active` with the same TC-ID; existing run snapshots are
    unchanged and future runs include it again if automated. UI: "Reactivate" on deprecated test cases. Editing
    deprecated test cases stays allowed.
11. **shadcn/ui components are vendored manually** in `frontend/src/components/ui` (the shadcn registry was not
    reachable from the build environment); `components.json` is present so `npx shadcn add` works later.
12. **Coverage exceptions (see `coverage/exceptions.yaml`).** Generated sqlc code, Postgres adapters and the process
    entrypoint are excluded from the backend Unit gate and verified by Integration/E2E; pages/components, query
    hooks and vendored UI are excluded from the frontend Unit gate and verified by Integration/Contract.
13. **Toolchain.** Go 1.26 (current `pgx`, `testcontainers-go` and `goose` releases require it); TypeScript 5.9
    (`openapi-typescript` does not support TypeScript 6 yet).
