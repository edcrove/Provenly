-- name: InsertTestRun :one
INSERT INTO test_runs (project_id, external_run_id, provider, provider_run_id, run_attempt, pipeline, branch, commit_sha, status, started_at, completed_at, report_sha256)
VALUES (@project_id, @external_run_id, @provider, @provider_run_id, @run_attempt, @pipeline, @branch, @commit_sha, @status, @started_at, @completed_at, @report_sha256)
ON CONFLICT (project_id, external_run_id) DO NOTHING
RETURNING id;

-- name: InsertExpectedCases :exec
INSERT INTO test_run_expected_cases (test_run_id, test_case_id)
SELECT @test_run_id, unnest(@test_case_ids::bigint[]);

-- name: InsertTestResults :copyfrom
INSERT INTO test_results (test_run_id, test_case_id, requested_test_case_id, correlation, test_name, class_name, suite_name, status, duration_ms, error_message, error_details)
VALUES (@test_run_id, @test_case_id, @requested_test_case_id, @correlation, @test_name, @class_name, @suite_name, @status, @duration_ms, @error_message, @error_details);

-- name: GetTestRun :one
SELECT r.*,
    (SELECT count(*) FROM test_run_expected_cases e WHERE e.test_run_id = r.id)::int AS expected_count,
    (SELECT count(*) FROM test_results t WHERE t.test_run_id = r.id)::int AS result_count
FROM test_runs r WHERE r.id = @id;

-- name: GetTestRunIDByExternalID :one
SELECT id FROM test_runs WHERE project_id = @project_id AND external_run_id = @external_run_id;

-- name: ListTestRuns :many
-- The page is chosen first: the per-run counts are only computed for its rows,
-- not for every row skipped by OFFSET.
SELECT r.*,
    (SELECT count(*) FROM test_run_expected_cases e WHERE e.test_run_id = r.id)::int AS expected_count,
    (SELECT count(*) FROM test_results t WHERE t.test_run_id = r.id)::int AS result_count
FROM test_runs r
WHERE r.id IN (
    SELECT p.id FROM test_runs p
    WHERE sqlc.narg('project_id')::bigint IS NULL OR p.project_id = sqlc.narg('project_id')::bigint
    ORDER BY p.id DESC LIMIT @page_limit OFFSET @page_offset
)
ORDER BY r.id DESC;

-- name: CountTestRuns :one
SELECT count(*) FROM test_runs
WHERE sqlc.narg('project_id')::bigint IS NULL OR project_id = sqlc.narg('project_id')::bigint;

-- name: ListRunResults :many
SELECT * FROM test_results
WHERE test_run_id = @test_run_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('correlation')::text IS NULL OR correlation = sqlc.narg('correlation')::text)
ORDER BY id
LIMIT @page_limit OFFSET @page_offset;

-- name: CountRunResults :one
SELECT count(*) FROM test_results
WHERE test_run_id = @test_run_id
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('correlation')::text IS NULL OR correlation = sqlc.narg('correlation')::text);

-- name: ListSummaryInputs :many
-- Snapshot TC-IDs (status NULL) and valid results of the given runs, in one read.
SELECT test_run_id, test_case_id, NULL::text AS status FROM test_run_expected_cases
WHERE test_run_id = ANY(@test_run_ids::bigint[])
UNION ALL
SELECT test_run_id, test_case_id::bigint, status FROM test_results
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
        (SELECT count(*) FROM test_results x WHERE x.test_run_id = r.id)::int AS result_count
    FROM test_runs r
    WHERE r.id IN (SELECT q.test_run_id FROM test_results q WHERE q.id IN (SELECT id FROM page))
)
SELECT sqlc.embed(t),
    r.project_id AS run_project_id, r.external_run_id, r.provider, r.provider_run_id, r.run_attempt, r.pipeline, r.branch, r.commit_sha,
    r.status AS run_status, r.created_at AS run_created_at, r.started_at AS run_started_at, r.completed_at AS run_completed_at,
    c.expected_count AS run_expected_count, c.result_count AS run_result_count
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
