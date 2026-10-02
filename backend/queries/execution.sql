-- name: InsertTestRun :one
INSERT INTO test_runs (external_run_id, provider, provider_run_id, run_attempt, pipeline, branch, commit_sha, status, started_at, completed_at, report_sha256)
VALUES (@external_run_id, @provider, @provider_run_id, @run_attempt, @pipeline, @branch, @commit_sha, @status, @started_at, @completed_at, @report_sha256)
ON CONFLICT (external_run_id) DO NOTHING
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
SELECT id FROM test_runs WHERE external_run_id = @external_run_id;

-- name: ListTestRuns :many
SELECT r.*,
    (SELECT count(*) FROM test_run_expected_cases e WHERE e.test_run_id = r.id)::int AS expected_count,
    (SELECT count(*) FROM test_results t WHERE t.test_run_id = r.id)::int AS result_count
FROM test_runs r
ORDER BY r.id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountTestRuns :one
SELECT count(*) FROM test_runs;

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
SELECT sqlc.embed(t),
    r.external_run_id, r.provider, r.provider_run_id, r.run_attempt, r.pipeline, r.branch, r.commit_sha,
    r.status AS run_status, r.created_at AS run_created_at, r.started_at AS run_started_at, r.completed_at AS run_completed_at,
    (SELECT count(*) FROM test_run_expected_cases e WHERE e.test_run_id = r.id)::int AS run_expected_count,
    (SELECT count(*) FROM test_results x WHERE x.test_run_id = r.id)::int AS run_result_count
FROM test_results t
JOIN test_runs r ON r.id = t.test_run_id
WHERE t.test_case_id = @test_case_id
ORDER BY t.id DESC
LIMIT @page_limit OFFSET @page_offset;

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
