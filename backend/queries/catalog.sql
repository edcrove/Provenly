-- name: CreateTestCase :one
INSERT INTO test_cases (title, description, expected_result, automated)
VALUES (@title, @description, @expected_result, @automated)
RETURNING *;

-- name: GetTestCase :one
SELECT * FROM test_cases WHERE id = @id;

-- name: LockTestCase :one
SELECT id FROM test_cases WHERE id = @id FOR UPDATE;

-- name: ListTestCases :many
SELECT * FROM test_cases
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountTestCases :one
SELECT count(*) FROM test_cases
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

-- name: UpdateTestCase :one
UPDATE test_cases SET
    title           = coalesce(sqlc.narg('title'), title),
    description     = coalesce(sqlc.narg('description'), description),
    expected_result = coalesce(sqlc.narg('expected_result'), expected_result),
    automated       = coalesce(sqlc.narg('automated'), automated),
    updated_at      = now()
WHERE id = @id
RETURNING *;

-- name: DeprecateTestCase :one
UPDATE test_cases SET
    status        = 'deprecated',
    deprecated_at = coalesce(deprecated_at, now()),
    updated_at    = CASE WHEN status = 'deprecated' THEN updated_at ELSE now() END
WHERE id = @id
RETURNING *;

-- name: ReactivateTestCase :one
UPDATE test_cases SET
    status        = 'active',
    deprecated_at = NULL,
    updated_at    = CASE WHEN status = 'active' THEN updated_at ELSE now() END
WHERE id = @id
RETURNING *;

-- name: ListIngestionView :many
-- One statement, so the expected universe (active AND automated) and the status
-- of the referenced TC-IDs come from the same snapshot: a deprecation committed
-- during an ingestion cannot put a TC in one and not the other.
SELECT id, status, (status = 'active' AND automated)::boolean AS expected, coalesce(id = ANY(@ids::bigint[]), false)::boolean AS referenced
FROM test_cases
WHERE (status = 'active' AND automated) OR id = ANY(@ids::bigint[])
ORDER BY id;

-- name: ListTestSteps :many
SELECT * FROM test_steps
WHERE test_case_id = @test_case_id
ORDER BY position
LIMIT @page_limit OFFSET @page_offset;

-- name: ListAllTestSteps :many
SELECT * FROM test_steps WHERE test_case_id = @test_case_id ORDER BY position;

-- name: CountTestSteps :one
SELECT count(*) FROM test_steps WHERE test_case_id = @test_case_id;

-- name: ShiftTestStepsDown :exec
UPDATE test_steps SET position = position + 1
WHERE test_case_id = @test_case_id AND position >= @from_position;

-- name: CreateTestStep :one
INSERT INTO test_steps (test_case_id, position, action, expected_result)
VALUES (@test_case_id, @position, @action, @expected_result)
RETURNING *;

-- name: UpdateTestStep :one
UPDATE test_steps SET
    action          = coalesce(sqlc.narg('action'), action),
    expected_result = coalesce(sqlc.narg('expected_result'), expected_result),
    updated_at      = now()
WHERE test_case_id = @test_case_id AND id = @id
RETURNING *;

-- name: DeleteTestStep :one
DELETE FROM test_steps WHERE test_case_id = @test_case_id AND id = @id
RETURNING position;

-- name: CloseTestStepGap :exec
UPDATE test_steps SET position = position - 1
WHERE test_case_id = @test_case_id AND position > @after_position;

-- name: SetTestStepPosition :exec
UPDATE test_steps SET position = @position, updated_at = now()
WHERE test_case_id = @test_case_id AND id = @id;
