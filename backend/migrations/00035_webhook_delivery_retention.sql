-- Module: integrations. Webhook delivery retention (card #51): finished deliveries older than
-- PROVENLY_WEBHOOK_DELIVERY_RETENTION_DAYS are purged in batches; this index finds them by completion time.
-- Audit events and run results are never purged.

-- +goose Up
CREATE INDEX webhook_deliveries_finished ON webhook_deliveries (completed_at) WHERE status <> 'pending';

-- +goose Down
DROP INDEX webhook_deliveries_finished;
