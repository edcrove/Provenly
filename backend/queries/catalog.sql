-- name: CreateTestCase :one
-- The number comes from the project's counter in the same statement: numbers
-- are assigned per project, in order, and never reused. No row when the
-- project does not exist.
WITH n AS (
    UPDATE projects SET next_number = next_number + 1
    WHERE id = @project_id
    RETURNING next_number - 1 AS number
)
INSERT INTO test_cases (project_id, number, title, description, expected_result, automated)
SELECT @project_id, n.number, @title, @description, @expected_result, @automated FROM n
RETURNING *;

-- name: GetTestCase :one
SELECT * FROM test_cases WHERE id = @id;

-- name: LockTestCase :one
SELECT id FROM test_cases WHERE id = @id FOR UPDATE;

-- name: ListTestCases :many
SELECT * FROM test_cases
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('project_id')::bigint IS NULL OR project_id = sqlc.narg('project_id')::bigint)
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountTestCases :one
SELECT count(*) FROM test_cases
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('project_id')::bigint IS NULL OR project_id = sqlc.narg('project_id')::bigint);

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
-- One statement, so the expected universe of the project (active AND automated)
-- and the status of the referenced numbers come from the same snapshot: a
-- deprecation committed during an ingestion cannot put a TC in one and not the other.
SELECT id, number, status, (status = 'active' AND automated)::boolean AS expected, coalesce(number = ANY(@numbers::bigint[]), false)::boolean AS referenced
FROM test_cases
WHERE project_id = @project_id AND ((status = 'active' AND automated) OR number = ANY(@numbers::bigint[]))
ORDER BY id;

-- name: ListTestCaseKeys :many
-- The identity (project and number) of the given test cases, for display.
SELECT id, project_id, number FROM test_cases WHERE id = ANY(@ids::bigint[]);

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

-- name: CreateProject :one
INSERT INTO projects (key, name, description)
VALUES (@key, @name, @description)
ON CONFLICT (key) DO NOTHING
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = @id;

-- name: GetProjectByKey :one
SELECT * FROM projects WHERE key = @key;

-- name: ListProjects :many
SELECT * FROM projects ORDER BY key LIMIT @page_limit OFFSET @page_offset;

-- name: CountProjects :one
SELECT count(*) FROM projects;

-- name: UpdateProject :one
UPDATE projects SET
    name        = coalesce(sqlc.narg('name'), name),
    description = coalesce(sqlc.narg('description'), description),
    updated_at  = now()
WHERE key = @key
RETURNING *;
