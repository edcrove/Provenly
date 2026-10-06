-- name: InsertAuditEvent :exec
INSERT INTO audit_events (actor, action, path, project_key, status, summary, test_case_key, ip, user_agent)
VALUES (@actor, @action, @path, sqlc.narg('project_key'), @status, sqlc.narg('summary'), sqlc.narg('test_case_key'),
        sqlc.narg('ip'), sqlc.narg('user_agent'));

-- name: ListAuditEvents :many
-- Newest first, narrowed by project, actor and/or test case.
SELECT * FROM audit_events
WHERE (sqlc.narg('project_key')::text IS NULL OR project_key = sqlc.narg('project_key'))
  AND (sqlc.narg('actor')::text IS NULL OR actor = sqlc.narg('actor'))
  AND (sqlc.narg('test_case_key')::text IS NULL OR test_case_key = sqlc.narg('test_case_key'))
ORDER BY id DESC LIMIT @page_limit OFFSET @page_offset;

-- name: CountAuditEvents :one
SELECT count(*) FROM audit_events
WHERE (sqlc.narg('project_key')::text IS NULL OR project_key = sqlc.narg('project_key'))
  AND (sqlc.narg('actor')::text IS NULL OR actor = sqlc.narg('actor'))
  AND (sqlc.narg('test_case_key')::text IS NULL OR test_case_key = sqlc.narg('test_case_key'));
