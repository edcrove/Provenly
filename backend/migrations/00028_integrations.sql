-- Module: integrations (Notion 16 Connectors, 17 Security; Planning #3, #21; MVP D4). Webhooks export events of a
-- project to an HTTPS endpoint, signed with a secret Provenly keeps encrypted (secrets package); every delivery is a
-- row retried with backoff until it succeeds or gives up. The GitHub connector mirrors a repository's issues with a
-- token kept encrypted. project_id holds catalog project ids by value: no cross-module foreign keys.

-- +goose Up
CREATE TABLE webhooks (
    id         BIGSERIAL   PRIMARY KEY,
    project_id BIGINT      NOT NULL,
    url        TEXT        NOT NULL CHECK (url ~ '^https?://' AND char_length(url) <= 2000),
    events     TEXT[]      NOT NULL CHECK (cardinality(events) >= 1 AND events <@ ARRAY['run.completed']::text[]),
    secret     TEXT        NOT NULL CHECK (secret LIKE 'v1:%'),
    active     BOOLEAN     NOT NULL DEFAULT true,
    created_by TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX webhooks_project ON webhooks (project_id);

CREATE TABLE webhook_deliveries (
    id               BIGSERIAL   PRIMARY KEY,
    webhook_id       BIGINT      NOT NULL REFERENCES webhooks (id),
    event            TEXT        NOT NULL CHECK (event IN ('run.completed', 'ping')),
    payload          JSONB       NOT NULL,
    status           TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'succeeded', 'failed')),
    attempts         INT         NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_status_code INT,
    last_error       TEXT        NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 1000),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ,
    CONSTRAINT webhook_deliveries_completed CHECK ((status = 'pending') = (completed_at IS NULL))
);
CREATE INDEX webhook_deliveries_due ON webhook_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX webhook_deliveries_webhook ON webhook_deliveries (webhook_id, id DESC);

CREATE TABLE github_connections (
    project_id     BIGINT      PRIMARY KEY,
    repository     TEXT        NOT NULL CHECK (repository ~ '^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$'),
    token          TEXT        NOT NULL CHECK (token LIKE 'v1:%'),
    labels         TEXT        NOT NULL DEFAULT '' CHECK (char_length(labels) <= 200),
    last_synced_at TIMESTAMPTZ,
    last_error     TEXT        NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 1000),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE github_connections;
DROP TABLE webhook_deliveries;
DROP TABLE webhooks;
