-- Module: identity. API keys let CI report runs without a person's account
-- (MVP D4, D11). A key belongs to one project and can only ingest into it.
-- Only a SHA-256 digest of the key is stored: it is shown once. Keys are never
-- deleted (revoked instead); only last use and revocation change.

-- +goose Up
CREATE TABLE api_keys (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id   BIGINT      NOT NULL REFERENCES projects (id),
    name         TEXT        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    prefix       TEXT        NOT NULL UNIQUE CHECK (prefix ~ '^pvk_[0-9a-f]{8}$'),
    token_sha256 BYTEA       NOT NULL UNIQUE CHECK (length(token_sha256) = 32),
    created_by   BIGINT      NOT NULL REFERENCES users (id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX api_keys_project_idx ON api_keys (project_id, id DESC);

-- +goose StatementBegin
CREATE FUNCTION api_keys_protect() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'API keys cannot be deleted; revoke instead';
    END IF;
    IF NEW.project_id <> OLD.project_id OR NEW.name <> OLD.name OR NEW.prefix <> OLD.prefix
        OR NEW.token_sha256 <> OLD.token_sha256 OR NEW.created_by <> OLD.created_by OR NEW.created_at <> OLD.created_at
        OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'an API key only records its last use and is revoked once';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER api_keys_protect BEFORE UPDATE OR DELETE ON api_keys
    FOR EACH ROW EXECUTE FUNCTION api_keys_protect();

-- +goose Down
DROP TABLE api_keys;
DROP FUNCTION api_keys_protect();
