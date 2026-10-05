-- name: InsertTestRun :one
INSERT INTO test_runs (project_id, external_run_id, provider, provider_run_id, run_attempt, pipeline, branch, commit_sha, status, started_at, completed_at, report_sha256, suite_key, suite_name, mode, started_by)
VALUES (@project_id, @external_run_id, @provider, @provider_run_id, @run_attempt, @pipeline, @branch, @commit_sha, @status, @started_at, @completed_at, @report_sha256, sqlc.narg('suite_key'), sqlc.narg('suite_name'), @mode, sqlc.narg('started_by'))
ON CONFLICT (project_id, external_run_id) DO NOTHING
RETURNING id;

-- name: InsertExpectedCases :exec
INSERT INTO test_run_expected_cases (test_run_id, test_case_id)
SELECT @test_run_id, unnest(@test_case_ids::bigint[]);

-- name: InsertTestResults :copyfrom
INSERT INTO test_results (test_run_id, test_case_id, requested_test_case_id, correlation, test_name, class_name, suite_name, status, duration_ms, error_message, error_details, attempt)
VALUES (@test_run_id, @test_case_id, @requested_test_case_id, @correlation, @test_name, @class_name, @suite_name, @status, @duration_ms, @error_message, @error_details, @attempt);

-- name: GetTestRun :one
SELECT r.*,
    (SELECT count(*) FROM test_run_expected_cases e WHERE e.test_run_id = r.id)::int AS expected_count,
    (SELECT count(*) FROM test_results t WHERE t.test_run_id = r.id)::int AS result_count,
    (SELECT count(*) FROM test_run_amendments a WHERE a.test_run_id = r.id)::int AS amendment_count
FROM test_runs r WHERE r.id = @id;

-- name: GetTestRunIDByExternalID :one
SELECT id FROM test_runs WHERE project_id = @project_id AND external_run_id = @external_run_id;

-- name: ListTestRuns :many
-- The page is chosen first: the per-run counts are only computed for its rows,
-- not for every row skipped by OFFSET.
SELECT r.*,
    (SELECT count(*) FROM test_run_expected_cases e WHERE e.test_run_id = r.id)::int AS expected_count,
    (SELECT count(*) FROM test_results t WHERE t.test_run_id = r.id)::int AS result_count,
    (SELECT count(*) FROM test_run_amendments a WHERE a.test_run_id = r.id)::int AS amendment_count
FROM test_runs r
WHERE r.id IN (
    SELECT p.id FROM test_runs p
    WHERE (sqlc.narg('project_ids')::bigint[] IS NULL OR p.project_id = ANY(sqlc.narg('project_ids')::bigint[]))
      AND (sqlc.narg('suite_key')::text IS NULL OR p.suite_key = sqlc.narg('suite_key')::text)
    ORDER BY p.id DESC LIMIT @page_limit OFFSET @page_offset
)
ORDER BY r.id DESC;

-- name: CountTestRuns :one
SELECT count(*) FROM test_runs
WHERE (sqlc.narg('project_ids')::bigint[] IS NULL OR project_id = ANY(sqlc.narg('project_ids')::bigint[]))
  AND (sqlc.narg('suite_key')::text IS NULL OR suite_key = sqlc.narg('suite_key')::text);

-- name: ListRunResults :many
-- retried: a later attempt of the same test exists in the run, so this one is not its logical result.
SELECT sqlc.embed(t), EXISTS (
    SELECT 1 FROM test_results x WHERE x.test_run_id = t.test_run_id AND x.suite_name = t.suite_name
      AND x.class_name = t.class_name AND x.test_name = t.test_name AND x.attempt > t.attempt
) AS retried
FROM test_results t
WHERE t.test_run_id = @test_run_id
  AND (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status')::text)
  AND (sqlc.narg('correlation')::text IS NULL OR t.correlation = sqlc.narg('correlation')::text)
ORDER BY t.id
LIMIT @page_limit OFFSET @page_offset;

-- name: CountRunResults :one
SELECT count(*) FROM test_results
WHERE test_run_id = @test_run_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('correlation')::text IS NULL OR correlation = sqlc.narg('correlation')::text);

-- name: ListSummaryInputs :many
-- Snapshot TC-IDs (kind 'expected'), amendments ('amended') and valid results ('result', with their status, the
-- test they belong to and their attempt) of the given runs, in one read.
SELECT test_run_id, test_case_id, 'expected' AS kind, NULL::text AS status, ''::text AS execution, 0 AS attempt
FROM test_run_expected_cases
WHERE test_run_id = ANY(@test_run_ids::bigint[])
UNION ALL
SELECT test_run_id, test_case_id, 'amended', NULL, '', 0 FROM test_run_amendments
WHERE test_run_id = ANY(@test_run_ids::bigint[])
UNION ALL
SELECT test_run_id, test_case_id::bigint, 'result', status, suite_name || chr(31) || class_name || chr(31) || test_name, attempt
FROM test_results
WHERE test_run_id = ANY(@test_run_ids::bigint[]) AND correlation = 'valid'
ORDER BY 1, 2;

-- name: ListDiagnosticResults :many
SELECT test_name, correlation, requested_test_case_id FROM test_results
WHERE test_run_id = @test_run_id AND correlation <> 'valid'
ORDER BY id;

-- name: ListResultsForTestCase :many
-- The page is chosen first (index on test_case_id, id DESC) and each run's counts
-- are computed once, not for every row skipped by OFFSET or repeated per result.
WITH page AS (
    SELECT p.id FROM test_results p WHERE p.test_case_id = @test_case_id
    ORDER BY p.id DESC LIMIT @page_limit OFFSET @page_offset
), counts AS (
    SELECT r.id,
        (SELECT count(*) FROM test_run_expected_cases e WHERE e.test_run_id = r.id)::int AS expected_count,
        (SELECT count(*) FROM test_results x WHERE x.test_run_id = r.id)::int AS result_count,
        (SELECT count(*) FROM test_run_amendments a WHERE a.test_run_id = r.id)::int AS amendment_count
    FROM test_runs r
    WHERE r.id IN (SELECT q.test_run_id FROM test_results q WHERE q.id IN (SELECT id FROM page))
)
SELECT sqlc.embed(t), EXISTS (
        SELECT 1 FROM test_results x WHERE x.test_run_id = t.test_run_id AND x.suite_name = t.suite_name
          AND x.class_name = t.class_name AND x.test_name = t.test_name AND x.attempt > t.attempt
    ) AS retried,
    r.project_id AS run_project_id, r.external_run_id, r.provider, r.provider_run_id, r.run_attempt, r.pipeline, r.branch, r.commit_sha,
    r.status AS run_status, r.created_at AS run_created_at, r.started_at AS run_started_at, r.completed_at AS run_completed_at,
    c.expected_count AS run_expected_count, c.result_count AS run_result_count, c.amendment_count AS run_amendment_count
FROM test_results t
JOIN test_runs r ON r.id = t.test_run_id
JOIN counts c ON c.id = r.id
WHERE t.id IN (SELECT id FROM page)
ORDER BY t.id DESC;

-- name: CountResultsForTestCase :one
SELECT count(*) FROM test_results WHERE test_case_id = @test_case_id;

-- name: InsertParseErrors :copyfrom
INSERT INTO test_run_parse_errors (test_run_id, case_index, test_name, message, persisted, severity)
VALUES (@test_run_id, @case_index, @test_name, @message, @persisted, @severity);

-- name: ListParseErrors :many
SELECT case_index, test_name, message, persisted, severity FROM test_run_parse_errors
WHERE test_run_id = @test_run_id
ORDER BY case_index
LIMIT @page_limit OFFSET @page_offset;

-- name: CountParseErrors :one
SELECT count(*) FROM test_run_parse_errors WHERE test_run_id = @test_run_id;

-- name: InsertAmendment :one
INSERT INTO test_run_amendments (test_run_id, test_case_id, amended_by, amended_by_username, reason)
VALUES (@test_run_id, @test_case_id, @amended_by, @amended_by_username, @reason)
ON CONFLICT (test_run_id, test_case_id) DO NOTHING
RETURNING *;

-- name: ListAmendments :many
SELECT * FROM test_run_amendments WHERE test_run_id = @test_run_id ORDER BY id
LIMIT @page_limit OFFSET @page_offset;

-- name: CountAmendments :one
SELECT count(*) FROM test_run_amendments WHERE test_run_id = @test_run_id;

-- name: LockTestRun :one
-- Locks a run until the transaction ends (manual recording and completion).
SELECT status, mode FROM test_runs WHERE id = @id FOR UPDATE;

-- name: IsInUniverse :one
-- Whether a test case is in a run's universe: its snapshot or its amendments.
SELECT EXISTS (SELECT 1 FROM test_run_expected_cases e WHERE e.test_run_id = sqlc.arg('run_id')::bigint AND e.test_case_id = sqlc.arg('case_id')::bigint)
    OR EXISTS (SELECT 1 FROM test_run_amendments a WHERE a.test_run_id = sqlc.arg('run_id')::bigint AND a.test_case_id = sqlc.arg('case_id')::bigint);

-- name: InsertManualResult :one
-- One recorded result of a running run; a re-test of the same test is its next attempt.
INSERT INTO test_results (test_run_id, test_case_id, requested_test_case_id, correlation, test_name, class_name, suite_name, status, duration_ms, error_message, error_details, attempt, recorded_by, failed_step)
SELECT @test_run_id, @test_case_id, @requested_test_case_id, 'valid', @test_name, @class_name, '', @status, sqlc.narg('duration_ms'), @error_message, '',
    coalesce(max(x.attempt), 0) + 1, sqlc.narg('recorded_by'), sqlc.narg('failed_step')
FROM test_results x WHERE x.test_run_id = @test_run_id AND x.class_name = @class_name AND x.test_name = @test_name
RETURNING *;

-- name: FinishTestRun :exec
UPDATE test_runs SET status = @status, completed_at = now() WHERE id = @id AND status = 'running';

-- name: ListLatestResults :many
-- The valid results of each given test case in the latest run that has one for it (status, test and attempt), to
-- read its latest status (requirement coverage).
SELECT t.test_case_id::bigint AS test_case_id, t.status, (t.suite_name || chr(31) || t.class_name || chr(31) || t.test_name)::text AS execution, t.attempt
FROM test_results t
WHERE t.correlation = 'valid' AND t.test_case_id = ANY(@test_case_ids::bigint[])
  AND t.test_run_id = (SELECT max(x.test_run_id) FROM test_results x WHERE x.test_case_id = t.test_case_id AND x.correlation = 'valid')
ORDER BY t.test_case_id, t.id;

-- name: ListLatestConclusive :many
-- For each given test case, its latest run with a conclusive logical status (passed, failed or error; skipped runs are
-- inconclusive) and that status. The logical status of a test case in a run is the highest attempt of each test,
-- aggregated failed > error > skipped > passed, as in summaries.
WITH last_attempts AS (
    SELECT DISTINCT ON (t.test_case_id, t.test_run_id, t.suite_name, t.class_name, t.test_name)
        t.test_case_id, t.test_run_id, t.status
    FROM test_results t
    WHERE t.correlation = 'valid' AND t.test_case_id = ANY(@test_case_ids::bigint[])
    ORDER BY t.test_case_id, t.test_run_id, t.suite_name, t.class_name, t.test_name, t.attempt DESC, t.id DESC
), per_run AS (
    SELECT a.test_case_id, a.test_run_id,
        CASE WHEN bool_or(a.status = 'failed') THEN 'failed' WHEN bool_or(a.status = 'error') THEN 'error'
             WHEN bool_or(a.status = 'skipped') THEN 'skipped' ELSE 'passed' END AS status
    FROM last_attempts a GROUP BY a.test_case_id, a.test_run_id
)
SELECT DISTINCT ON (p.test_case_id) p.test_case_id::bigint AS test_case_id, p.test_run_id, p.status::text AS status
FROM per_run p WHERE p.status <> 'skipped'
ORDER BY p.test_case_id, p.test_run_id DESC;

-- name: ListLastExecuted :many
-- When each given test case last had a valid result (the creation time of its latest run with one).
SELECT t.test_case_id::bigint AS test_case_id, max(r.created_at)::timestamptz AS last_executed_at
FROM test_results t JOIN test_runs r ON r.id = t.test_run_id
WHERE t.correlation = 'valid' AND t.test_case_id = ANY(@test_case_ids::bigint[])
GROUP BY t.test_case_id;

-- name: ListFlakyCounts :many
-- In a project's latest runs, how many runs each test case was flaky in: one of its tests passed on its last attempt
-- after a failed or errored one. Manual re-tests are never flaky.
WITH runs AS (
    SELECT id FROM test_runs WHERE project_id = @project_id ORDER BY id DESC LIMIT @window_runs
), tests AS (
    SELECT t.test_case_id, t.test_run_id,
        (array_agg(t.status ORDER BY t.attempt DESC, t.id DESC))[1] AS last_status,
        bool_or(t.status IN ('failed', 'error')) AS any_failure
    FROM test_results t
    WHERE t.test_run_id IN (SELECT id FROM runs) AND t.correlation = 'valid' AND t.class_name <> 'provenly-manual'
    GROUP BY t.test_case_id, t.test_run_id, t.suite_name, t.class_name, t.test_name
)
SELECT x.test_case_id::bigint AS test_case_id, count(DISTINCT x.test_run_id)::int AS flaky_runs
FROM tests x WHERE x.last_status = 'passed' AND x.any_failure
GROUP BY x.test_case_id
ORDER BY flaky_runs DESC, x.test_case_id
LIMIT @max_items;

-- name: CountRunEvents :one
SELECT count(*)::int FROM test_run_events WHERE test_run_id = @test_run_id;

-- name: InsertRunEvent :execrows
-- Appends one live event; an event id already received for the run is a duplicate delivery and is skipped (0 rows).
INSERT INTO test_run_events (test_run_id, event_id, sequence, event_type, test_name, requested_test_case_id, test_case_id, status, occurred_at, attempt)
VALUES (@test_run_id, @event_id, @sequence, @event_type, @test_name, sqlc.narg('requested_test_case_id'), sqlc.narg('test_case_id'), sqlc.narg('status'), @occurred_at, @attempt)
ON CONFLICT (test_run_id, event_id) DO NOTHING;

-- name: ListRunEvents :many
SELECT * FROM test_run_events WHERE test_run_id = @test_run_id ORDER BY sequence, id;

-- name: CompleteLiveRun :exec
-- The final report completes a running live run: its execution status, completion time and report digest.
UPDATE test_runs SET status = @status, completed_at = now(), report_sha256 = @report_sha256
WHERE id = @id AND mode = 'live' AND status = 'running';
