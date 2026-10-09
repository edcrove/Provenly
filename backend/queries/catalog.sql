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
-- Locks the test case (and its steps' order) until the transaction ends; returns its current version.
SELECT version FROM test_cases WHERE id = @id FOR UPDATE;

-- name: ListTestCases :many
-- classified holds distinct dimension:value pairs that must all hold (AND); a test case has one value per
-- dimension, so two values of one dimension match nothing.
SELECT * FROM test_cases
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('project_ids')::bigint[] IS NULL OR project_id = ANY(sqlc.narg('project_ids')::bigint[]))
  AND (sqlc.narg('tag')::text IS NULL OR EXISTS (SELECT 1 FROM test_case_tags t WHERE t.test_case_id = test_cases.id AND t.tag = sqlc.narg('tag')::text))
  AND (coalesce(cardinality(@classified::text[]), 0) = 0 OR (
      SELECT count(*) FROM test_case_classifications c
      JOIN classification_dimensions d ON d.id = c.dimension_id
      JOIN classification_values v ON v.id = c.value_id
      WHERE c.test_case_id = test_cases.id AND d.key || ':' || v.key = ANY(@classified::text[])
  ) = cardinality(@classified::text[]))
  AND (sqlc.narg('suite_id')::bigint IS NULL OR EXISTS (SELECT 1 FROM test_suite_cases m WHERE m.suite_id = sqlc.narg('suite_id')::bigint AND m.test_case_id = test_cases.id))
  AND (sqlc.narg('automated')::boolean IS NULL OR automated = sqlc.narg('automated')::boolean)
  AND (sqlc.narg('number')::bigint IS NULL OR number = sqlc.narg('number')::bigint)
  AND (sqlc.narg('search')::text IS NULL OR title ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
       OR number = sqlc.narg('search_number')::bigint)
ORDER BY id DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountTestCases :one
SELECT count(*) FROM test_cases
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('project_ids')::bigint[] IS NULL OR project_id = ANY(sqlc.narg('project_ids')::bigint[]))
  AND (sqlc.narg('tag')::text IS NULL OR EXISTS (SELECT 1 FROM test_case_tags t WHERE t.test_case_id = test_cases.id AND t.tag = sqlc.narg('tag')::text))
  AND (coalesce(cardinality(@classified::text[]), 0) = 0 OR (
      SELECT count(*) FROM test_case_classifications c
      JOIN classification_dimensions d ON d.id = c.dimension_id
      JOIN classification_values v ON v.id = c.value_id
      WHERE c.test_case_id = test_cases.id AND d.key || ':' || v.key = ANY(@classified::text[])
  ) = cardinality(@classified::text[]))
  AND (sqlc.narg('suite_id')::bigint IS NULL OR EXISTS (SELECT 1 FROM test_suite_cases m WHERE m.suite_id = sqlc.narg('suite_id')::bigint AND m.test_case_id = test_cases.id))
  AND (sqlc.narg('automated')::boolean IS NULL OR automated = sqlc.narg('automated')::boolean)
  AND (sqlc.narg('number')::bigint IS NULL OR number = sqlc.narg('number')::bigint)
  AND (sqlc.narg('search')::text IS NULL OR title ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
       OR number = sqlc.narg('search_number')::bigint);

-- name: ListTestCaseIDs :many
-- The ids of every test case the filters select (a suite's selection for a run), ascending.
SELECT id FROM test_cases
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('project_ids')::bigint[] IS NULL OR project_id = ANY(sqlc.narg('project_ids')::bigint[]))
  AND (sqlc.narg('tag')::text IS NULL OR EXISTS (SELECT 1 FROM test_case_tags t WHERE t.test_case_id = test_cases.id AND t.tag = sqlc.narg('tag')::text))
  AND (coalesce(cardinality(@classified::text[]), 0) = 0 OR (
      SELECT count(*) FROM test_case_classifications c
      JOIN classification_dimensions d ON d.id = c.dimension_id
      JOIN classification_values v ON v.id = c.value_id
      WHERE c.test_case_id = test_cases.id AND d.key || ':' || v.key = ANY(@classified::text[])
  ) = cardinality(@classified::text[]))
  AND (sqlc.narg('suite_id')::bigint IS NULL OR EXISTS (SELECT 1 FROM test_suite_cases m WHERE m.suite_id = sqlc.narg('suite_id')::bigint AND m.test_case_id = test_cases.id))
  AND (sqlc.narg('automated')::boolean IS NULL OR automated = sqlc.narg('automated')::boolean)
ORDER BY id;

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
-- project_ids NULL means every project (administrators); otherwise only those. search (LIKE-escaped) finds a key or a
-- name containing it, any case.
SELECT * FROM projects
WHERE (sqlc.narg('project_ids')::bigint[] IS NULL OR id = ANY(sqlc.narg('project_ids')::bigint[]))
  AND (sqlc.narg('search')::text IS NULL OR key ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
       OR name ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\')
ORDER BY key LIMIT @page_limit OFFSET @page_offset;

-- name: CountProjects :one
SELECT count(*) FROM projects
WHERE (sqlc.narg('project_ids')::bigint[] IS NULL OR id = ANY(sqlc.narg('project_ids')::bigint[]))
  AND (sqlc.narg('search')::text IS NULL OR key ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\'
       OR name ILIKE '%' || sqlc.narg('search')::text || '%' ESCAPE '\');

-- name: UpdateProject :one
UPDATE projects SET
    name        = coalesce(sqlc.narg('name'), name),
    description = coalesce(sqlc.narg('description'), description),
    updated_at  = now()
WHERE key = @key
RETURNING *;

-- name: ListDimensions :many
-- A project's classification dimensions: built-ins first in their seeded order, then the project's own by key.
SELECT * FROM classification_dimensions WHERE project_id = @project_id
ORDER BY built_in DESC, id;

-- name: ListDimensionValues :many
SELECT v.* FROM classification_values v
JOIN classification_dimensions d ON d.id = v.dimension_id
WHERE d.project_id = @project_id
ORDER BY v.dimension_id, v.position, v.id;

-- name: CreateDimension :one
INSERT INTO classification_dimensions (project_id, key, name)
VALUES (@project_id, @key, @name)
ON CONFLICT (project_id, key) DO NOTHING
RETURNING *;

-- name: UpdateDimension :one
UPDATE classification_dimensions SET
    name        = coalesce(sqlc.narg('name'), name),
    archived_at = CASE WHEN sqlc.narg('archived')::boolean IS NULL THEN archived_at
                       WHEN sqlc.narg('archived')::boolean THEN coalesce(archived_at, now())
                       ELSE NULL END
WHERE project_id = @project_id AND key = @key
RETURNING *;

-- name: CreateDimensionValue :one
-- Appended after the dimension's last value; no row when the key already exists in the dimension.
INSERT INTO classification_values (dimension_id, key, name, position)
SELECT @dimension_id, @key, @name, coalesce(max(position), 0) + 1 FROM classification_values WHERE dimension_id = @dimension_id
ON CONFLICT (dimension_id, key) DO NOTHING
RETURNING *;

-- name: UpdateDimensionValue :one
UPDATE classification_values SET
    name        = coalesce(sqlc.narg('name'), name),
    archived_at = CASE WHEN sqlc.narg('archived')::boolean IS NULL THEN archived_at
                       WHEN sqlc.narg('archived')::boolean THEN coalesce(archived_at, now())
                       ELSE NULL END
WHERE dimension_id = @dimension_id AND key = @key
RETURNING *;

-- name: ListTestCaseTags :many
SELECT test_case_id, tag FROM test_case_tags WHERE test_case_id = ANY(@ids::bigint[])
ORDER BY test_case_id, tag;

-- name: ListTestCaseClassifications :many
-- The classification of the given test cases as dimension and value keys.
SELECT c.test_case_id, d.key AS dimension, v.key AS value
FROM test_case_classifications c
JOIN classification_dimensions d ON d.id = c.dimension_id
JOIN classification_values v ON v.id = c.value_id
WHERE c.test_case_id = ANY(@ids::bigint[])
ORDER BY c.test_case_id, d.id;

-- name: DeleteTestCaseTags :exec
-- Removes the tags not in keep (all of them when keep is empty).
DELETE FROM test_case_tags WHERE test_case_id = @test_case_id AND NOT (tag = ANY(coalesce(@keep::text[], '{}')));

-- name: AddTestCaseTags :exec
INSERT INTO test_case_tags (test_case_id, tag)
SELECT @test_case_id, unnest(@tags::text[])
ON CONFLICT DO NOTHING;

-- name: SetTestCaseClassification :exec
-- Sets the value of one dimension; unchanged when it already has that value (so the version does not advance).
INSERT INTO test_case_classifications (test_case_id, project_id, dimension_id, value_id)
VALUES (@test_case_id, @project_id, @dimension_id, @value_id)
ON CONFLICT (test_case_id, dimension_id) DO UPDATE SET value_id = EXCLUDED.value_id
WHERE test_case_classifications.value_id <> EXCLUDED.value_id;

-- name: ClearTestCaseClassification :exec
DELETE FROM test_case_classifications WHERE test_case_id = @test_case_id AND dimension_id = @dimension_id;

-- name: ListSuites :many
-- A project's suites by key, with the number of test cases a static suite lists.
SELECT s.*, (SELECT count(*) FROM test_suite_cases c WHERE c.suite_id = s.id)::int AS case_count
FROM test_suites s WHERE s.project_id = @project_id ORDER BY s.key;

-- name: GetSuite :one
SELECT s.*, (SELECT count(*) FROM test_suite_cases c WHERE c.suite_id = s.id)::int AS case_count
FROM test_suites s WHERE s.project_id = @project_id AND s.key = @key;

-- name: CreateSuite :one
INSERT INTO test_suites (project_id, key, name, description, kind, query_tag, query_classified)
VALUES (@project_id, @key, @name, @description, @kind, sqlc.narg('query_tag'), @query_classified)
ON CONFLICT (project_id, key) DO NOTHING
RETURNING id;

-- name: UpdateSuite :one
UPDATE test_suites SET
    name             = coalesce(sqlc.narg('name'), name),
    description      = coalesce(sqlc.narg('description'), description),
    query_tag        = CASE WHEN @set_query::boolean THEN sqlc.narg('query_tag') ELSE query_tag END,
    query_classified = CASE WHEN @set_query::boolean THEN @query_classified::text[] ELSE query_classified END,
    archived_at      = CASE WHEN sqlc.narg('archived')::boolean IS NULL THEN archived_at
                            WHEN sqlc.narg('archived')::boolean THEN coalesce(archived_at, now())
                            ELSE NULL END,
    updated_at       = now()
WHERE project_id = @project_id AND key = @key
RETURNING id;

-- name: ListSuiteCaseIDs :many
SELECT test_case_id FROM test_suite_cases WHERE suite_id = @suite_id ORDER BY test_case_id;

-- name: DeleteSuiteCases :exec
-- Removes the members not in keep (all of them when keep is empty).
DELETE FROM test_suite_cases WHERE suite_id = @suite_id AND NOT (test_case_id = ANY(coalesce(@keep::bigint[], '{}')));

-- name: AddSuiteCases :exec
INSERT INTO test_suite_cases (suite_id, project_id, test_case_id)
SELECT @suite_id, @project_id, unnest(@test_case_ids::bigint[])
ON CONFLICT DO NOTHING;

-- name: ListProjectCaseIDs :many
-- Which of the given ids are test cases of the project.
SELECT id FROM test_cases WHERE project_id = @project_id AND id = ANY(@ids::bigint[]) ORDER BY id;

-- name: ListRequirements :many
-- A project's requirements (optionally only those a test case covers), newest first, with their linked test cases.
SELECT r.*, coalesce((SELECT array_agg(l.test_case_id ORDER BY l.test_case_id) FROM requirement_test_cases l WHERE l.requirement_id = r.id), '{}')::bigint[] AS test_case_ids
FROM requirements r
WHERE r.project_id = @project_id
  AND (sqlc.narg('test_case_id')::bigint IS NULL OR EXISTS (SELECT 1 FROM requirement_test_cases x WHERE x.requirement_id = r.id AND x.test_case_id = sqlc.narg('test_case_id')::bigint))
ORDER BY r.id DESC;

-- name: GetRequirement :one
SELECT r.*, coalesce((SELECT array_agg(l.test_case_id ORDER BY l.test_case_id) FROM requirement_test_cases l WHERE l.requirement_id = r.id), '{}')::bigint[] AS test_case_ids
FROM requirements r WHERE r.project_id = @project_id AND r.id = @id;

-- name: NextNativeRequirementNumber :one
-- Takes the next R-<n> of a project's native requirements from its counter (the row lock serializes concurrent
-- creations; numbers are never reused).
UPDATE projects SET next_requirement_number = next_requirement_number + 1
WHERE id = @project_id
RETURNING (next_requirement_number - 1)::bigint AS number;

-- name: UpsertRequirement :one
-- Creates a requirement, or (when sync is true) updates the mirrored one with the same provider and external id.
-- xmax = 0 tells a fresh insert from an update.
INSERT INTO requirements (project_id, provider, external_id, title, description, url, provider_status, last_synced_at)
VALUES (@project_id, @provider, @external_id, @title, @description, @url, @provider_status, sqlc.narg('last_synced_at'))
ON CONFLICT (project_id, provider, external_id) DO UPDATE SET
    title = EXCLUDED.title, description = EXCLUDED.description, url = EXCLUDED.url,
    provider_status = EXCLUDED.provider_status, last_synced_at = EXCLUDED.last_synced_at, updated_at = now()
WHERE @sync::boolean
RETURNING id, (xmax = 0)::boolean AS created;

-- name: UpdateRequirement :one
UPDATE requirements SET
    title           = coalesce(sqlc.narg('title'), title),
    description     = coalesce(sqlc.narg('description'), description),
    url             = coalesce(sqlc.narg('url'), url),
    provider_status = coalesce(sqlc.narg('provider_status'), provider_status),
    archived_at     = CASE WHEN sqlc.narg('archived')::boolean IS NULL THEN archived_at
                           WHEN sqlc.narg('archived')::boolean THEN coalesce(archived_at, now())
                           ELSE NULL END,
    updated_at      = now()
WHERE project_id = @project_id AND id = @id
RETURNING id;

-- name: DeleteRequirementLinks :exec
DELETE FROM requirement_test_cases WHERE requirement_id = @requirement_id AND NOT (test_case_id = ANY(coalesce(@keep::bigint[], '{}')));

-- name: AddRequirementLinks :exec
INSERT INTO requirement_test_cases (requirement_id, project_id, test_case_id)
SELECT @requirement_id, @project_id, unnest(@test_case_ids::bigint[])
ON CONFLICT DO NOTHING;

-- name: ListIssues :many
-- A project's issues (optionally only those linked to a test case, or in one state), newest first, with their linked
-- test cases.
SELECT i.*, coalesce((SELECT array_agg(l.test_case_id ORDER BY l.test_case_id) FROM issue_test_cases l WHERE l.issue_id = i.id), '{}')::bigint[] AS test_case_ids
FROM issues i
WHERE i.project_id = @project_id
  AND (sqlc.narg('state')::text IS NULL OR i.state = sqlc.narg('state')::text)
  AND (sqlc.narg('test_case_id')::bigint IS NULL OR EXISTS (SELECT 1 FROM issue_test_cases x WHERE x.issue_id = i.id AND x.test_case_id = sqlc.narg('test_case_id')::bigint))
ORDER BY i.id DESC;

-- name: GetIssue :one
SELECT i.*, coalesce((SELECT array_agg(l.test_case_id ORDER BY l.test_case_id) FROM issue_test_cases l WHERE l.issue_id = i.id), '{}')::bigint[] AS test_case_ids
FROM issues i WHERE i.project_id = @project_id AND i.id = @id;

-- name: NextNativeIssueNumber :one
-- Takes the next I-<n> of a project's native issues from its counter (the row lock serializes concurrent creations).
UPDATE projects SET next_issue_number = next_issue_number + 1
WHERE id = @project_id
RETURNING (next_issue_number - 1)::bigint AS number;

-- name: UpsertIssue :one
-- Creates an issue, or (when sync is true) updates the mirrored one with the same provider and external id; closed_at
-- follows the state (kept while it stays closed).
INSERT INTO issues (project_id, provider, external_id, title, description, url, state, provider_status, closed_at, last_synced_at)
VALUES (@project_id, @provider, @external_id, @title, @description, @url, @state, @provider_status,
    CASE WHEN @state::text = 'closed' THEN now() END, sqlc.narg('last_synced_at'))
ON CONFLICT (project_id, provider, external_id) DO UPDATE SET
    title = EXCLUDED.title, description = EXCLUDED.description, url = EXCLUDED.url, state = EXCLUDED.state,
    provider_status = EXCLUDED.provider_status, last_synced_at = EXCLUDED.last_synced_at,
    closed_at = CASE WHEN EXCLUDED.state = 'closed' THEN coalesce(issues.closed_at, now()) END, updated_at = now()
WHERE @sync::boolean
RETURNING id, (xmax = 0)::boolean AS created;

-- name: UpdateIssue :one
UPDATE issues SET
    title           = coalesce(sqlc.narg('title'), title),
    description     = coalesce(sqlc.narg('description'), description),
    url             = coalesce(sqlc.narg('url'), url),
    provider_status = coalesce(sqlc.narg('provider_status'), provider_status),
    state           = coalesce(sqlc.narg('state'), state),
    closed_at       = CASE WHEN coalesce(sqlc.narg('state'), state) = 'closed' THEN coalesce(closed_at, now()) END,
    updated_at      = now()
WHERE project_id = @project_id AND id = @id
RETURNING id;

-- name: DeleteIssueLinks :exec
DELETE FROM issue_test_cases WHERE issue_id = @issue_id AND NOT (test_case_id = ANY(coalesce(@keep::bigint[], '{}')));

-- name: AddIssueLinks :exec
INSERT INTO issue_test_cases (issue_id, project_id, test_case_id)
SELECT @issue_id, @project_id, unnest(@test_case_ids::bigint[])
ON CONFLICT DO NOTHING;

-- name: LockLinkParent :exec
-- Serializes replacements of one requirement's, issue's or static suite's test case set: concurrent replacements run
-- one after the other, so the set always ends as one caller's list (never a union of two).
SELECT pg_advisory_xact_lock(hashtextextended(@kind::text, @id::bigint));
