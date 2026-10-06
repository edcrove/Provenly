-- Module: identity. Account offboarding (card #61, refined decision 11): an administrator deactivates a user (every
-- session stops at once; project API keys belong to their project and keep working) and gives a forgotten password a
-- single-use reset link. Only a SHA-256 digest of the link's token is stored, like invitations.

-- +goose Up
ALTER TABLE users ADD COLUMN deactivated_at TIMESTAMPTZ;

CREATE TABLE password_resets (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users (id),
    token_sha256 BYTEA       NOT NULL UNIQUE CHECK (length(token_sha256) = 32),
    -- The administrator who made the link; NULL when the break-glass command made it.
    created_by   BIGINT      REFERENCES users (id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    CONSTRAINT password_resets_expiry CHECK (expires_at > created_at),
    CONSTRAINT password_resets_used CHECK (used_at IS NULL OR used_at >= created_at)
);
CREATE INDEX password_resets_user ON password_resets (user_id) WHERE used_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION password_resets_protect() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'password resets cannot be deleted (reset %)', OLD.id;
    END IF;
    IF (NEW.id, NEW.user_id, NEW.token_sha256, NEW.created_by, NEW.created_at, NEW.expires_at)
        IS DISTINCT FROM (OLD.id, OLD.user_id, OLD.token_sha256, OLD.created_by, OLD.created_at, OLD.expires_at)
       OR OLD.used_at IS NOT NULL THEN
        RAISE EXCEPTION 'a password reset is only ever marked used, once (reset %)', OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER password_resets_protect BEFORE UPDATE OR DELETE ON password_resets
    FOR EACH ROW EXECUTE FUNCTION password_resets_protect();

-- +goose Down
DROP TABLE password_resets;
DROP FUNCTION password_resets_protect();
ALTER TABLE users DROP COLUMN deactivated_at;
