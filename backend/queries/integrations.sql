-- name: CreateWebhook :one
INSERT INTO webhooks (project_id, url, events, secret, created_by)
VALUES (@project_id, @url, @events::text[], @secret, @created_by)
RETURNING *;

-- name: ListWebhooks :many
SELECT * FROM webhooks WHERE project_id = @project_id ORDER BY id;

-- name: GetWebhook :one
SELECT * FROM webhooks WHERE project_id = @project_id AND id = @id;

-- name: GetWebhookByID :one
SELECT * FROM webhooks WHERE id = @id;

-- name: UpdateWebhook :one
UPDATE webhooks SET
    url        = coalesce(sqlc.narg('url'), url),
    events     = coalesce(sqlc.narg('events')::text[], events),
    active     = coalesce(sqlc.narg('active'), active),
    updated_at = now()
WHERE project_id = @project_id AND id = @id
RETURNING id;

-- name: ListSubscribedWebhooks :many
-- The active webhooks of a project subscribed to an event.
SELECT id FROM webhooks WHERE project_id = @project_id AND active AND @event::text = ANY(events) ORDER BY id;

-- name: InsertDelivery :one
INSERT INTO webhook_deliveries (webhook_id, event, payload) VALUES (@webhook_id, @event, @payload) RETURNING id;

-- name: ClaimDueDeliveries :many
-- Leases up to max_items due deliveries for a minute: concurrent workers skip each other's rows, and a worker that
-- dies leaves its rows due again once the lease ends.
UPDATE webhook_deliveries d SET next_attempt_at = now() + interval '1 minute'
WHERE d.id IN (
    SELECT x.id FROM webhook_deliveries x
    WHERE x.status = 'pending' AND x.next_attempt_at <= now()
    ORDER BY x.id LIMIT @max_items FOR UPDATE SKIP LOCKED
)
RETURNING d.*;

-- name: FinishAttempt :exec
-- Records one delivery attempt: still pending (retry at next_attempt_at), succeeded or failed for good.
UPDATE webhook_deliveries SET
    status = @status, attempts = @attempts, last_status_code = sqlc.narg('last_status_code'), last_error = @last_error,
    next_attempt_at = @next_attempt_at, completed_at = CASE WHEN @status::text = 'pending' THEN NULL ELSE now() END
WHERE id = @id;

-- name: ListDeliveries :many
SELECT * FROM webhook_deliveries WHERE webhook_id = @webhook_id ORDER BY id DESC LIMIT @page_limit OFFSET @page_offset;

-- name: CountDeliveries :one
SELECT count(*) FROM webhook_deliveries WHERE webhook_id = @webhook_id;

-- name: LastDeliveries :many
-- The latest delivery of each webhook.
SELECT DISTINCT ON (webhook_id) * FROM webhook_deliveries WHERE webhook_id = ANY(@webhook_ids::bigint[]) ORDER BY webhook_id, id DESC;

-- name: UpsertGitHubConnection :exec
INSERT INTO github_connections (project_id, repository, token, labels)
VALUES (@project_id, @repository, @token, @labels)
ON CONFLICT (project_id) DO UPDATE SET repository = EXCLUDED.repository, token = EXCLUDED.token, labels = EXCLUDED.labels,
    last_error = '', updated_at = now();

-- name: GetGitHubConnection :one
SELECT * FROM github_connections WHERE project_id = @project_id;

-- name: DeleteGitHubConnection :execrows
DELETE FROM github_connections WHERE project_id = @project_id;

-- name: RecordGitHubSync :exec
UPDATE github_connections SET last_synced_at = coalesce(sqlc.narg('synced_at'), last_synced_at), last_error = @last_error, updated_at = now()
WHERE project_id = @project_id;
