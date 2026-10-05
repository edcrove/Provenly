-- Module: audit (Incubator, Project & Authorization). Every authenticated change made through the API (a successful
-- POST, PUT, PATCH or DELETE) is one row: who (a username or an API key), which operation (method and route), the
-- request path and the project it addressed. Request bodies are never stored (they can carry secrets). Rows are
-- append-only.

-- +goose Up
CREATE TABLE audit_events (
    id          BIGSERIAL   PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor       TEXT        NOT NULL CHECK (char_length(actor) BETWEEN 1 AND 200),
    action      TEXT        NOT NULL CHECK (action ~ '^(POST|PUT|PATCH|DELETE) /' AND char_length(action) <= 300),
    path        TEXT        NOT NULL CHECK (char_length(path) BETWEEN 1 AND 2000),
    project_key TEXT        CHECK (char_length(project_key) <= 50),
    status      INT         NOT NULL CHECK (status BETWEEN 200 AND 299)
);
CREATE INDEX audit_events_project ON audit_events (project_key, id DESC);
CREATE INDEX audit_events_actor ON audit_events (actor, id DESC);

-- +goose StatementBegin
CREATE FUNCTION audit_events_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit events are append-only (event %)', OLD.id;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER audit_events_append_only BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_append_only();

-- +goose Down
DROP TABLE audit_events;
DROP FUNCTION audit_events_append_only();
