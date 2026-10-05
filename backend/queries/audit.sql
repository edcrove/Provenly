-- name: InsertAuditEvent :exec
INSERT INTO audit_events (actor, action, path, project_key, status)
VALUES (@actor, @action, @path, sqlc.narg('project_key'), @status);

-- name: ListAuditEvents :many
-- Newest first, narrowed by project and/or actor.
SELECT * FROM audit_events
WHERE (sqlc.narg('project_key')::text IS NULL OR project_key = sqlc.narg('project_key'))
  AND (sqlc.narg('actor')::text IS NULL OR actor = sqlc.narg('actor'))
ORDER BY id DESC LIMIT @page_limit OFFSET @page_offset;

-- name: CountAuditEvents :one
SELECT count(*) FROM audit_events
WHERE (sqlc.narg('project_key')::text IS NULL OR project_key = sqlc.narg('project_key'))
  AND (sqlc.narg('actor')::text IS NULL OR actor = sqlc.narg('actor'));
