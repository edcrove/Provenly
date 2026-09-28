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
   (2) conflicts with the frozen invariant "the snapshot is never modified after creation", so it needs a Decision
   Register change first. Proposed shape: an explicit, audited *snapshot amendment* per run (who, when, which
   TC-ID, only TC-IDs that already have valid results in the run), never an automatic recomputation.
3. **Deprecated TC-IDs in results.** Following "testCaseId when the TC-ID is valid", results that reference a
   deprecated test case keep only `requestedTestCaseId` + `correlation=deprecated`; they are therefore not listed in
   that test case's history.
4. **Malformed rules.** `tc-id` accepts `153` or `TC-153`; `0`, leading zeros, non-digits, >18 digits, an empty
   value, or several *different* ids (property or name) are `malformed`.
5. **Parse errors.** A testcase without `name` or with an invalid `time` is reported in `parseErrors` and not
   persisted; the rest of the batch continues.
6. **Replay response.** An idempotent replay returns `200` with the existing run and the diagnostics stored at
   creation; nothing is re-parsed into the database.
7. **Run lifecycle in the POC.** Runs are created synchronously when the final report is ingested and stored as
   `completed`; `startedAt` is the earliest `testsuite@timestamp` when present.
8. **Steps.** At most 100 steps per test case (so one page of the paginated steps listing always holds them all);
   reorder is `PUT /steps/order` with the full permutation of step ids.
9. **Summary rounding.** Percentages are 0..100 rounded to 2 decimals; 0 when the denominator is 0.
10. **Deprecation is one-way** in the POC (no re-activation endpoint); editing a deprecated test case is allowed.
11. **shadcn/ui components are vendored manually** in `frontend/src/components/ui` (the shadcn registry was not
    reachable from the build environment); `components.json` is present so `npx shadcn add` works later.
12. **Coverage exceptions (see `coverage/exceptions.yaml`).** Generated sqlc code, Postgres adapters and the process
    entrypoint are excluded from the backend Unit gate and verified by Integration/E2E; pages/components, query
    hooks and vendored UI are excluded from the frontend Unit gate and verified by Integration/Contract.
13. **Toolchain.** Go 1.26 (current `pgx`, `testcontainers-go` and `goose` releases require it); TypeScript 5.9
    (`openapi-typescript` does not support TypeScript 6 yet).
