-- Module: identity. Personal access tokens (card #62): a person's read-only token for scripts and MCP
-- clients, limited to some of their projects and always expiring (at most 365 days). Only a SHA-256 digest is
-- stored: the token is shown once. Tokens are never deleted (revoked instead, also when the user is deactivated);
-- only their last use and revocation change. The projects a token covers are fixed when it is created.

-- +goose Up
CREATE TABLE personal_access_tokens (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users (id),
    name         TEXT        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    prefix       TEXT        NOT NULL UNIQUE CHECK (prefix ~ '^pvly_pat_[0-9a-f]{8}$'),
    token_sha256 BYTEA       NOT NULL UNIQUE CHECK (length(token_sha256) = 32),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '365 days')
);
CREATE INDEX personal_access_tokens_user_idx ON personal_access_tokens (user_id, id DESC);

CREATE TABLE personal_access_token_projects (
    token_id   BIGINT NOT NULL REFERENCES personal_access_tokens (id),
    project_id BIGINT NOT NULL REFERENCES projects (id),
    PRIMARY KEY (token_id, project_id)
);

-- +goose StatementBegin
CREATE FUNCTION personal_access_tokens_protect() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'personal access tokens cannot be deleted; revoke instead';
    END IF;
    IF NEW.user_id <> OLD.user_id OR NEW.name <> OLD.name OR NEW.prefix <> OLD.prefix
        OR NEW.token_sha256 <> OLD.token_sha256 OR NEW.created_at <> OLD.created_at OR NEW.expires_at <> OLD.expires_at
        OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'a personal access token only records its last use and is revoked once';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION personal_access_token_projects_protect() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'the projects of a personal access token are fixed when it is created';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER personal_access_tokens_protect BEFORE UPDATE OR DELETE ON personal_access_tokens
    FOR EACH ROW EXECUTE FUNCTION personal_access_tokens_protect();
CREATE TRIGGER personal_access_token_projects_protect BEFORE UPDATE OR DELETE ON personal_access_token_projects
    FOR EACH ROW EXECUTE FUNCTION personal_access_token_projects_protect();

-- +goose Down
DROP TABLE personal_access_token_projects;
DROP TABLE personal_access_tokens;
DROP FUNCTION personal_access_token_projects_protect();
DROP FUNCTION personal_access_tokens_protect();
